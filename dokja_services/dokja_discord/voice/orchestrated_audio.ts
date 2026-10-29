import { randomUUID } from "node:crypto";
import { buildBridgePayload } from "../transport/bridge.js";

export async function voiceAudioRequest(endpoint: string | undefined,
  action: "synthesize" | "transcribe" | "wakeword",
  input: { guildId: string; userId: string; channelId: string; text?: string;
    audio_bytes_b64?: string; hotword?: string }, signal?: AbortSignal,
): Promise<Record<string, unknown>> {
  if (!endpoint) throw new Error("Voice requires the orchestrator bridge endpoint");
  const body = buildBridgePayload({ eventType: `voice.${action}`,
    userId: input.userId, channelId: input.channelId, guildId: input.guildId,
    messageId: randomUUID(),
  });
  body.payload.voice = { text: input.text, audio_bytes_b64: input.audio_bytes_b64,
    hotword: input.hotword, language: "pt-BR" };
  const response = await fetch(endpoint, { method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${process.env.DOKJA_GATEWAY_INGRESS_TOKEN ?? ""}` },
    body: JSON.stringify(body), signal: AbortSignal.any([AbortSignal.timeout(15_000), ...(signal ? [signal] : [])]),
  });
  if (!response.ok) throw new Error(`Voice orchestration failed: ${response.status}`);
  const result = await response.json() as { status: string; voice?: Record<string, unknown> };
  if (result.status !== "ok" || !result.voice) throw new Error("Voice request was refused");
  return result.voice;
}
