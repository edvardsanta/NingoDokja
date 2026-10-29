import { randomUUID } from "node:crypto";
import type { DiscordCommandContext, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController } from "../voice/voice.js";
import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";

type Observer = { id: string; channel: string; origin: string; users: Set<string>; withdrawn: Set<string> };

// Each participant grants only their own consent. Rejoining after process restart
// requires fresh consent; this registry is never a durable authorization source.
export function observerCommands(endpoint: string, timeout: number, voice?: DiscordVoiceController): DiscordSlashCommand[] {
  const rooms = new Map<string, Observer>();
  const operations = new Set<string>();
  async function send(context: DiscordCommandContext, room: Observer, type: string, extra = {}) {
    const body = buildBridgePayload({eventType:type,userId:context.userId,guildId:context.guildId,
      channelId:room.origin,messageId:randomUUID()});
    body.payload.realtime={session_id:room.id,voice_channel_id:room.channel,...extra};
    const result=await dispatchToOrchestratorResponse(endpoint, Math.min(timeout,5000), body);
    if (result.realtime?.accepted !== true) throw new Error("Observer request was not authorized or accepted");
  }
  async function perform(context: DiscordCommandContext, name: string) {
    if (!voice?.getVoiceChannelId || !context.guildId) throw new Error("Observer requires a guild voice controller");
    const guild = context.guildId;
    let room = rooms.get(guild);
    if (name === "conversation_stop") {
      if (!room || !room.users.has(context.userId)) throw new Error("Only a consenting participant can stop this observer");
      rooms.delete(guild);room.users.clear();
      await voice.stopConversation({guildId:guild});
      await send(context,room,"realtime.stop");
      await context.reply("Conversation observer stopped; ephemeral transcripts deleted.");return;
    }
    if (name === "conversation_leave") {
      if (!room) throw new Error("No observer is active");
      room.users.delete(context.userId);room.withdrawn.add(context.userId);
      await send(context,room,"realtime.leave");
      await context.reply("Your transcription consent was withdrawn and your ephemeral transcripts deleted.");return;
    }
    const channel = await voice.getVoiceChannelId(guild,context.userId);
    if (name === "conversation_start") {
      if (room) throw new Error("An observer already exists; use /conversation_join to consent");
      room={id:randomUUID(),channel,origin:context.channelId,users:new Set(),withdrawn:new Set()};rooms.set(guild,room);
      try {
        await send(context,room,"realtime.start");
        room.users.add(context.userId);
        const selected=room;
        await voice.startConversation({guildId:guild,userId:context.userId,originChannelId:room.origin,
          consentingUsers:room.users,async onTurn(){},
          onStopped() {
            if (rooms.get(guild)!==selected) return;
            rooms.delete(guild);selected.users.clear();
            void send(context,selected,"realtime.stop").catch(()=>{});
            void context.updateReply("Conversation observer stopped. The session no longer accepts audio; ephemeral journal deletion was requested.").catch(()=>{});
          },
          async onObservation(observation) {
            if (rooms.get(guild)!==selected || !selected.users.has(observation.userId)) return;
            try {
              await send({...context,userId:observation.userId},selected,"realtime.observe",{
                capture_sequence:observation.sequence,message_id:`${selected.id}:${observation.sequence}`,text:observation.text,
                timestamp_start:observation.startedAt,timestamp_end:observation.endedAt,
                transcription_status:"final",revision:1,
              });
            } catch(error) {
              selected.users.clear();rooms.delete(guild);
              await voice.stopConversation({guildId:guild});
              await send(context,selected,"realtime.stop").catch(()=>{});
              await context.updateReply("Observer stopped because transcript delivery failed. Audio capture is disabled; deletion was requested and remaining ephemeral data expires after inactivity.").catch(()=>{});
              throw error;
            }
          },
        });
      } catch(error) {rooms.delete(guild);room.users.clear();await send(context,room,"realtime.stop").catch(()=>{});throw error;}
      await context.reply("Transcription observer active. Only explicitly consenting participants are captured. Use /conversation_join to opt in, /conversation_leave to withdraw, or /conversation_stop to end and delete. Open Realtime in Dokja Desktop to view segment-final transcripts. No raw audio is retained; transcripts expire after 15 minutes of inactivity. Ningo will not speak in this mode.");return;
    }
    if (!room || room.channel!==channel || context.channelId!==room.origin) throw new Error("Join the observer voice channel and use the original text channel");
    if (room.withdrawn.has(context.userId)) throw new Error("Start a new observer session before consenting again; queued audio from this session is invalidated");
    await send(context,room,"realtime.join");
    if (rooms.get(guild)!==room) throw new Error("Observer ended while joining");
    room.users.add(context.userId);
    await context.reply("You consented to ephemeral transcription in this observer session. Use /conversation_leave to withdraw.");
  }
  return ["conversation_start","conversation_join","conversation_leave","conversation_stop"].map(name=>({
    name,description:({conversation_start:"Start an observer for consenting speakers; Ningo stays silent.",conversation_join:"Consent to transcription in the active observer session.",conversation_leave:"Withdraw consent and delete your ephemeral transcripts.",conversation_stop:"Stop the observer and delete its ephemeral transcripts."} as Record<string,string>)[name],
    defer:true,
    async handle(context: DiscordCommandContext) {
      const guild=context.guildId??"";
      if (operations.has(guild)) {await context.reply("An observer operation is already in progress",{ephemeral:true});return;}
      operations.add(guild);
      try {await perform(context,name);} catch(error) {await context.reply(error instanceof Error?error.message:"Observer failed",{ephemeral:true});}
      finally {operations.delete(guild);}
    },
  }));
}
