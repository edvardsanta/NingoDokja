import { observerMessage } from "../messages/observer.js";
import { randomUUID } from "node:crypto";
import type { DiscordCommandContext, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController, VoiceObservation } from "../voice/voice.js";
import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";

type Observer = { id: string; channel: string; origin: string; active: boolean };
type Speaker = { userId: string; userName?: string; guildId?: string };

// Whether the orchestrator accepted it; a transport failure still throws.
async function realtimeRequest(endpoint: string, timeout: number, speaker: Speaker, room: Observer, type: string, extra = {}): Promise<boolean> {
  const body = buildBridgePayload({eventType:type,userId:speaker.userId,userName:speaker.userName,guildId:speaker.guildId,
    channelId:room.origin,messageId:randomUUID()});
  body.payload.realtime={session_id:room.id,voice_channel_id:room.channel,...extra};
  const result=await dispatchToOrchestratorResponse(endpoint, Math.min(timeout,5000), body);
  return result.realtime?.accepted === true;
}
async function accepted(request: Promise<boolean>): Promise<void> {
  if (!await request) throw new Error("Realtime request was not accepted");
}

const observed = (room: Observer, observation: VoiceObservation) => ({
  capture_sequence:observation.sequence,message_id:`${room.id}:${observation.sequence}`,text:observation.text,
  timestamp_start:observation.startedAt,timestamp_end:observation.endedAt,
  transcription_status:"final",revision:1,
});

// One line of Ningo's spoken reply, revised as each sentence is said.
export type SpokenReplyMirror = { said(sentence: string): void };
export type VoiceChatMirror = {
  heard(observation: VoiceObservation): void;
  // Ningo's reply to the turn ending at this capture sequence; it is placed right after it.
  reply(sequence: number): SpokenReplyMirror;
  stop(): void;
};

// The engine refuses a line spanning more than 30 s; a long reply keeps its first 30 s of span.
const MAX_LINE_SPAN_MS = 30_000;
const MAX_LINE_TEXT = 8000;

// Mirrors a voice chat Ningo answers into the owner's Desktop realtime view: the same ephemeral,
// channel-wide journal an observer keeps, marked voice_chat. Segments are sent one at a time, in
// order. A failure only ends the mirror; the voice chat itself goes on.
export async function startVoiceChatMirror(endpoint: string, timeout: number, starter: Speaker & { channelId: string },
  voice: Pick<DiscordVoiceController, "getVoiceChannelId">, error: (...args: unknown[]) => void = console.error): Promise<VoiceChatMirror | undefined> {
  if (!voice.getVoiceChannelId || !starter.guildId) return undefined;
  let room: Observer;
  try {
    room = {id:randomUUID(),channel:await voice.getVoiceChannelId(starter.guildId, starter.userId),origin:starter.channelId,active:true};
    await accepted(realtimeRequest(endpoint, timeout, starter, room, "realtime.start", {mode:"voice_chat"}));
  } catch (failure) { error("[dokja-discord] voice chat transcript mirror unavailable", failure); return undefined; }
  let queue = Promise.resolve();
  let replies = 0;
  const send = (speaker: Speaker, type: string, extra: Record<string, unknown>) => {
    queue = queue.then(async () => {
      if (!room.active) return;
      await accepted(realtimeRequest(endpoint, timeout, speaker, room, type, extra));
    }).catch((failure) => { error("[dokja-discord] voice chat transcript mirror stopped", failure); stop(); });
  };
  const stop = () => {
    if (!room.active) return;
    room.active = false;
    queue = queue.then(() => realtimeRequest(endpoint, timeout, starter, room, "realtime.stop")).catch(() => {});
  };
  return {
    heard(observation) {
      if (!room.active) return;
      send({...starter,userId:observation.userId,userName:observation.userName}, "realtime.observe", observed(room, observation));
    },
    reply(sequence) {
      const id = `${room.id}:ningo:${++replies}`;
      const started = Date.now();
      let text = "";
      let revision = 0;
      return {
        said(sentence) {
          const words = sentence.trim();
          if (!room.active || !words || text.length >= MAX_LINE_TEXT) return;
          text = `${text} ${words}`.trim().slice(0, MAX_LINE_TEXT);
          revision++;
          const ended = Math.min(Date.now(), started + MAX_LINE_SPAN_MS);
          send(starter, "realtime.observe", {
            capture_sequence:sequence,message_id:id,text,speaker_role:"ningo",
            timestamp_start:new Date(started).toISOString(),timestamp_end:new Date(ended).toISOString(),
            transcription_status:"final",revision,
          });
        },
      };
    },
    stop,
  };
}

// Sessions are bound to a guild voice channel and stop when the listener stops.
export function observerCommands(endpoint: string, timeout: number, voice?: DiscordVoiceController, locale = "pt-BR"): DiscordSlashCommand[] {
  const message = (key: Parameters<typeof observerMessage>[1]) => observerMessage(locale, key);
  const rooms = new Map<string, Observer>();
  const operations = new Set<string>();
  async function send(context: DiscordCommandContext, room: Observer, type: string, extra = {}) {
    if (!await realtimeRequest(endpoint, timeout, context, room, type, extra)) throw new Error(message("notAccepted"));
  }
  async function perform(context: DiscordCommandContext, name: string) {
    if (!voice?.getVoiceChannelId || !context.guildId) throw new Error(message("requiresVoice"));
    const guild = context.guildId;
    let room = rooms.get(guild);
    if (name === "conversation_stop") {
      if (!room) throw new Error(message("inactive"));
      const channel = await voice.getVoiceChannelId(guild, context.userId);
      if (channel !== room.channel || context.channelId !== room.origin) throw new Error(message("joinChannel"));
      rooms.delete(guild);room.active=false;
      await voice.stopConversation({guildId:guild});
      await send(context,room,"realtime.stop");
      await context.reply(message("stopped"));return;
    }
    const channel = await voice.getVoiceChannelId(guild,context.userId);
    if (name === "conversation_start") {
      if (room) throw new Error(message("alreadyActive"));
      room={id:randomUUID(),channel,origin:context.channelId,active:true};rooms.set(guild,room);
      try {
        await send(context,room,"realtime.start");
        const selected=room;
        await voice.startConversation({guildId:guild,userId:context.userId,originChannelId:room.origin,
          async onTurn(){},
          onStopped() {
            if (rooms.get(guild)!==selected) return;
            rooms.delete(guild);selected.active=false;
            void send(context,selected,"realtime.stop").catch(()=>{});
            void context.updateReply(message("disconnected")).catch(()=>{});
          },
          async onObservation(observation) {
            if (rooms.get(guild)!==selected || !selected.active) return;
            try {
              await send({...context,userId:observation.userId,userName:observation.userName},selected,"realtime.observe",observed(selected,observation));
            } catch(error) {
              selected.active=false;rooms.delete(guild);
              await voice.stopConversation({guildId:guild});
              await send(context,selected,"realtime.stop").catch(()=>{});
              await context.updateReply(message("deliveryFailed")).catch(()=>{});
              throw error;
            }
          },
        });
      } catch(error) {rooms.delete(guild);room.active=false;await send(context,room,"realtime.stop").catch(()=>{});throw error;}
      await context.reply(message("active"));return;
    }
  }
  return (["conversation_start","conversation_stop"] as const).map(name=>({
    name,description: observerMessage("en", name),
    defer:true,
    async handle(context: DiscordCommandContext) {
      const guild=context.guildId??"";
      if (operations.has(guild)) {await context.reply(message("busy"),{ephemeral:true});return;}
      operations.add(guild);
      try {await perform(context,name);} catch(error) {await context.reply(error instanceof Error?error.message:message("failed"),{ephemeral:true});}
      finally {operations.delete(guild);}
    },
  }));
}
