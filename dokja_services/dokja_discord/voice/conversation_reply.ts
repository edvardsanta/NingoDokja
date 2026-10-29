import { randomUUID } from "node:crypto";
import { buildBridgePayload } from "../transport/bridge.js";
import type { VoiceTurnContext } from "./live_conversation.js";

// Only complete sentences from the dedicated local voice provider become speech.
// Ordinary CLI previews remain isolated and are never forwarded here.
export async function conversationReply(endpoint: string, timeoutMs: number, input: {
  guildId?: string; originChannelId: string; turn: VoiceTurnContext;
  voiceSessionId?: string; voiceStartedBy?: string;
  onSpeech?(sentence: string): Promise<void>;
}): Promise<string> {
  const { turn } = input;
  turn.signal.throwIfAborted();
  const body = buildBridgePayload({ eventType: "message.created", userId: turn.userId,
    userName: turn.userName, channelId: input.originChannelId, guildId: input.guildId,
    content: turn.turns[turn.turns.length - 1].text, messageId: turn.trace?.id ?? randomUUID(),
    sessionKey: `discord-voice:${input.guildId}:${turn.channelId}`,
  });
  body.payload.voiceTurns = turn.turns.map(({userId,userName,text})=>({userId,userName,text}));
  body.payload.voiceChannelId = turn.channelId;
  body.payload.voiceSessionId = input.voiceSessionId;
  body.payload.voiceStartedBy = input.voiceStartedBy;
  turn.trace?.mark("bridge_requested");
  const response = await fetch(`${endpoint.replace(/\/$/, "")}/voice/chat/stream`, {
    method: "POST", headers: { "content-type": "application/json",
      authorization: `Bearer ${process.env.DOKJA_GATEWAY_INGRESS_TOKEN ?? ""}` },
    body: JSON.stringify(body), signal: AbortSignal.any([turn.signal, AbortSignal.timeout(timeoutMs)]),
  });
  if (!response.ok || !response.body) throw new Error(`Voice reply failed: ${response.status}`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let decided = false;
  let accepted = false;
  let spoken = "";
  try {
    while (true) {
      const {value,done} = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      if (buffer.length > 262144) throw new Error("Voice reply frame is too large");
      let newline: number;
      while ((newline=buffer.indexOf("\n"))>=0) {
        const line = buffer.slice(0,newline); buffer=buffer.slice(newline+1);
        if (!line.trim()) continue;
        const frame = JSON.parse(line) as { heartbeat?: boolean; accepted?: boolean; speech?: string; status?: string; reply?: string; error?: string };
        if (frame.heartbeat) continue;
        if (typeof frame.accepted === "boolean") {
          turn.trace?.mark(frame.accepted ? "reply_accepted" : "reply_passive");
          turn.decide?.(frame.accepted);
          decided = true;
          accepted = frame.accepted;
          continue;
        }
        if (typeof frame.speech === "string") {
          turn.signal.throwIfAborted();
          if (!decided || !accepted) throw new Error("Ineligible voice turn returned speech");
          turn.trace?.mark("usable_sentence_received");
          if (input.onSpeech) await input.onSpeech(frame.speech);
          spoken += `${frame.speech} `;
          continue;
        }
        turn.signal.throwIfAborted();
        if (frame.status!=="ok" || (frame.reply !== undefined && typeof frame.reply!=="string")) throw new Error("Voice reply was refused");
        if (decided && !accepted && frame.reply?.trim()) throw new Error("Ineligible voice turn returned speech");
        if (!decided) turn.decide?.(Boolean(frame.reply?.trim()));
        turn.trace?.mark("final_reply_received");
        const reply = frame.reply ?? "";
        return spoken && reply.startsWith(spoken.trim()) ? reply.slice(spoken.trim().length).trim() : reply;
      }
    }
    throw new Error("Voice reply stream ended without a final reply");
  } finally { await reader.cancel(); reader.releaseLock(); }
}
