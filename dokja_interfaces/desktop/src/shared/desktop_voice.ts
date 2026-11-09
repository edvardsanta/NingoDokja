// Desktop voice in Talk: a spoken message becomes text, is sent as an ordinary
// conversation message, and the kept reply is read aloud. These shapes bound
// what crosses the window boundary for each of those steps.
const BASE64 = /^[A-Za-z0-9+/]+={0,2}$/;

export const VOICE_SESSION = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
export const VOICE_LANGUAGES = ["pt-BR", "en"] as const;
export type VoiceLanguage = (typeof VOICE_LANGUAGES)[number];
// One utterance: at most 8 s of 16 kHz mono PCM16 in a 44-byte WAV, as base64.
export const MAX_UTTERANCE_SAMPLES = 16000 * 8;
export const MAX_UTTERANCE_BASE64 = Math.ceil((44 + MAX_UTTERANCE_SAMPLES * 2) / 3) * 4;
// What may be read aloud: a kept reply, and the speech made from it.
export const MAX_SPEAK_TEXT = 32000;
export const MAX_SPEECH_BASE64 = Math.ceil((8 * 1024 * 1024) / 3) * 4;
const MAX_TRANSCRIPT = 8000;

const language = (raw: unknown): raw is VoiceLanguage => VOICE_LANGUAGES.includes(raw as VoiceLanguage);

export function transcribeInput(raw: Record<string, unknown>): Record<string, unknown> | undefined {
  const audio = raw.audio_bytes_b64;
  if (typeof raw.voice_session_id !== "string" || !VOICE_SESSION.test(raw.voice_session_id)
    || typeof audio !== "string" || audio.length > MAX_UTTERANCE_BASE64 || !BASE64.test(audio)
    || !language(raw.language)) return undefined;
  return { voice_session_id: raw.voice_session_id, audio_bytes_b64: audio, audio_format: "wav", language: raw.language };
}

export function transcript(body: Record<string, unknown>): { text: string } {
  if (typeof body.text !== "string" || body.text.length > MAX_TRANSCRIPT) throw new Error("Invalid transcript");
  return { text: body.text.trim() };
}

export function speakInput(raw: Record<string, unknown>): Record<string, unknown> | undefined {
  if (typeof raw.text !== "string" || raw.text.trim() === "" || raw.text.length > MAX_SPEAK_TEXT || !language(raw.language)) return undefined;
  return { text: raw.text, language: raw.language };
}

export type Speech = { spoken: string; audio_bytes_b64?: string };

export function speech(body: Record<string, unknown>): Speech {
  if (typeof body.spoken !== "string" || body.spoken.length > MAX_SPEAK_TEXT) throw new Error("Invalid speech");
  const audio = body.audio_bytes_b64;
  if (body.spoken === "") return { spoken: "" };
  if (typeof audio !== "string" || audio.length > MAX_SPEECH_BASE64 || !BASE64.test(audio)) throw new Error("Invalid speech audio");
  return { spoken: body.spoken, audio_bytes_b64: audio };
}
