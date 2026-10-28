import { randomUUID } from "node:crypto";
import { buildBridgePayload } from "../transport/bridge.js";
import type { VoiceTurnContext } from "./live_conversation.js";

// Heartbeats let the server detect a disconnected/aborted requester and cancel
// inference even if its CLI has not emitted a token yet. Only a validated final
// reply can become speech; speculative previews are never spoken.
export async function conversationReply(endpoint: string, timeoutMs: number, input: {
  guildId?: string; originChannelId: string; turn: VoiceTurnContext;
  voiceSessionId?: string; voiceStartedBy?: string;
}): Promise<string> {
  const { turn } = input;
  turn.signal.throwIfAborted();
  const body = buildBridgePayload({ eventType: "message.created", userId: turn.userId,
    userName: turn.userName, channelId: input.originChannelId, guildId: input.guildId,
    content: turn.turns[turn.turns.length - 1].text, messageId: randomUUID(),
    sessionKey: `discord-voice:${input.guildId}:${turn.channelId}`,
  });
  body.payload.voiceTurns = turn.turns.map(({userId,userName,text})=>({userId,userName,text}));
  body.payload.voiceChannelId = turn.channelId;
  body.payload.voiceSessionId = input.voiceSessionId;
  body.payload.voiceStartedBy = input.voiceStartedBy;
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
        const frame = JSON.parse(line) as { heartbeat?: boolean; accepted?: boolean; status?: string; reply?: string; error?: string };
        if (frame.heartbeat) continue;
        if (typeof frame.accepted === "boolean") {
          turn.decide?.(frame.accepted);
          decided = true;
          accepted = frame.accepted;
          continue;
        }
        turn.signal.throwIfAborted();
        if (frame.status!=="ok" || (frame.reply !== undefined && typeof frame.reply!=="string")) throw new Error("Voice reply was refused");
        if (decided && !accepted && frame.reply?.trim()) throw new Error("Ineligible voice turn returned speech");
        if (!decided) turn.decide?.(Boolean(frame.reply?.trim()));
        return frame.reply ?? "";
      }
    }
    throw new Error("Voice reply stream ended without a final reply");
  } finally { await reader.cancel(); reader.releaseLock(); }
}
