import { speech, transcript, VOICE_SESSION, type VoiceLanguage } from "../../shared/desktop_voice.js";
import { isRecord } from "../../shared/records.js";
import type { Transport } from "../../shared/transport.js";
import { PcmSegments, audioBase64 } from "./pcm_segments.js";
import { BrowserVoiceAudio, type VoiceAudio, type VoiceAudioFactory } from "./microphone.js";

export type VoiceState = "idle" | "preparing" | "listening" | "thinking" | "speaking";
export type VoiceIssue = "voice_permission_denied" | "voice_capture_failed" | "voice_request_failed" | "voice_speech_failed";
export type VoiceCallbacks = { state(value: VoiceState): void; issue(value: VoiceIssue): void };
// Sends what was said as an ordinary conversation message; resolves to the kept reply, if any.
export type Converse = (text: string) => Promise<string | undefined>;

const TRANSCRIBE_TIMEOUT_MS = 30_000;
const SPEAK_TIMEOUT_MS = 60_000;

// One utterance at a time: capture pauses while it is transcribed, answered and read aloud.
// No service endpoints or provider credentials are available to this controller.
export class LiveDesktopVoice {
  private active = false;
  private busy = false;
  private session?: string;
  private audio?: VoiceAudio;
  private segments = new PcmSegments();
  constructor(private transport: Transport, private callbacks: VoiceCallbacks, private converse: Converse,
    private factory: VoiceAudioFactory = (samples, ended) => new BrowserVoiceAudio(samples, ended)) {}

  async start(language: VoiceLanguage): Promise<void> {
    if (this.active) return;
    this.active = true; this.callbacks.state("preparing");
    try {
      const reply = await this.transport.request("voice.desktop.start", {}, { timeoutMs: 15000 });
      if (!reply.ok || !isRecord(reply.result) || typeof reply.result.voice_session_id !== "string" || !VOICE_SESSION.test(reply.result.voice_session_id)) throw new Error("Voice session unavailable");
      const session = reply.result.voice_session_id;
      if (!this.active) { void this.closeRemote(session); return; }
      this.session = session;
      this.audio = this.factory(samples => {
        if (!this.active || this.busy) return;
        const wav = this.segments.push(samples);
        if (wav) void this.utterance(session, language, wav);
      }, () => this.stop());
      await this.audio.start();
      if (this.active && this.session === session) this.callbacks.state("listening");
    } catch (error) {
      if (!this.active) return;
      this.stop();
      this.callbacks.issue(error instanceof Error && error.name === "NotAllowedError" ? "voice_permission_denied" : "voice_capture_failed");
    }
  }

  stop(): void {
    this.active = false; this.busy = false; this.segments.reset();
    this.audio?.stop(); this.audio = undefined;
    const session = this.session; this.session = undefined;
    if (session) void this.closeRemote(session);
    this.callbacks.state("idle");
  }

  private current(session: string): boolean { return this.active && this.session === session; }

  private async closeRemote(session: string): Promise<void> {
    try { await this.transport.request("voice.desktop.stop", { voice_session_id: session }); } catch { /* Local capture is already stopped. */ }
  }

  private async utterance(session: string, language: VoiceLanguage, wav: Uint8Array): Promise<void> {
    this.busy = true; this.segments.reset(); this.callbacks.state("thinking");
    try {
      const heard = await this.transport.request("voice.transcribe", {
        voice_session_id: session, language, audio_bytes_b64: audioBase64(wav),
      }, { timeoutMs: TRANSCRIBE_TIMEOUT_MS });
      if (!this.current(session)) return;
      if (!heard.ok || !isRecord(heard.result)) throw new Error("Transcription failed");
      const { text } = transcript(heard.result);
      if (!text) return;
      const reply = await this.converse(text);
      if (!this.current(session) || !reply) return;
      await this.speak(session, language, reply);
    } catch {
      if (this.current(session)) { this.stop(); this.callbacks.issue("voice_request_failed"); }
    } finally {
      if (this.current(session)) { this.busy = false; this.segments.reset(); this.callbacks.state("listening"); }
    }
  }

  // A reply that cannot be read aloud is still on screen; listening goes on.
  private async speak(session: string, language: VoiceLanguage, reply: string): Promise<void> {
    try {
      const answer = await this.transport.request("voice.reply.speak", { text: reply, language }, { timeoutMs: SPEAK_TIMEOUT_MS });
      if (!this.current(session)) return;
      if (!answer.ok || !isRecord(answer.result)) throw new Error("Speech failed");
      const prepared = speech(answer.result);
      if (!prepared.audio_bytes_b64) return;
      this.callbacks.state("speaking");
      await this.audio?.play(prepared.audio_bytes_b64);
    } catch {
      if (this.current(session)) this.callbacks.issue("voice_speech_failed");
    }
  }
}
