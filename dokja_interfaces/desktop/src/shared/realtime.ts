import { isRecord } from "./records.js";
export type Observation = {message_id:string;session_id:string;speaker_id:string;text:string;timestamp_start:string;timestamp_end:string;revision:number;transcription_status:"final"};
export type ObserverSession = {session_id:string;guild_id:string;channel_id:string;origin_channel_id:string;consenting_participants:string[];messages:Observation[]};
export type ObserverSnapshot = {sessions:ObserverSession[]};
function short(value: unknown,max=128): string { return typeof value==="string"?value.slice(0,max):""; }
export function observerSnapshot(raw: unknown): ObserverSnapshot {
 if (!isRecord(raw)||!Array.isArray(raw.sessions)) throw new Error("Invalid observer snapshot");
 return {sessions:raw.sessions.slice(0,32).filter(isRecord).map(s=>({
  session_id:short(s.session_id),guild_id:short(s.guild_id),channel_id:short(s.channel_id),origin_channel_id:short(s.origin_channel_id),
  consenting_participants:Array.isArray(s.consenting_participants)?s.consenting_participants.slice(0,16).map(v=>short(v)):[],
  messages:Array.isArray(s.messages)?s.messages.slice(-512).filter(isRecord).filter(m=>m.transcription_status==="final").map(m=>({
   message_id:short(m.message_id),session_id:short(m.session_id),speaker_id:short(m.speaker_id),
   text:short(m.text,8000),timestamp_start:short(m.timestamp_start,64),timestamp_end:short(m.timestamp_end,64),
   revision:typeof m.revision==="number"&&Number.isSafeInteger(m.revision)?m.revision:1,transcription_status:"final" as const,
  })):[],
 }))};
}
