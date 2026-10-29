import { observerMessage } from "../messages/observer.js";
import { randomUUID } from "node:crypto";
import type { DiscordCommandContext, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";
import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";

type Observer = { id: string; channel: string; origin: string; active: boolean };

// Sessions are bound to a guild voice channel and stop when the listener stops.
export function observerCommands(endpoint: string, timeout: number, voice?: DiscordVoiceController, locale = "pt-BR"): DiscordSlashCommand[] {
  const message = (key: Parameters<typeof observerMessage>[1]) => observerMessage(locale, key);
  const rooms = new Map<string, Observer>();
  const operations = new Set<string>();
  async function send(context: DiscordCommandContext, room: Observer, type: string, extra = {}) {
    const body = buildBridgePayload({eventType:type,userId:context.userId,guildId:context.guildId,
      channelId:room.origin,messageId:randomUUID()});
    body.payload.realtime={session_id:room.id,voice_channel_id:room.channel,...extra};
    const result=await dispatchToOrchestratorResponse(endpoint, Math.min(timeout,5000), body);
    if (result.realtime?.accepted !== true) throw new Error(message("notAccepted"));
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
              await send({...context,userId:observation.userId},selected,"realtime.observe",{
                capture_sequence:observation.sequence,message_id:`${selected.id}:${observation.sequence}`,text:observation.text,
                timestamp_start:observation.startedAt,timestamp_end:observation.endedAt,
                transcription_status:"final",revision:1,
              });
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
    name,description: message(name),
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
