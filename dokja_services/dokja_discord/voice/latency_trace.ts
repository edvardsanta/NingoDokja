import { randomUUID } from "node:crypto";

export type VoiceTiming = {
  traceId: string;
  stage: string;
  elapsedMs: number;
  sentenceId?: string;
};

// Monotonic elapsed time; never attach transcript, participant or room data.
export class VoiceLatencyTrace {
  readonly id = randomUUID();
  private readonly started: number;
  constructor(private sink: (event: VoiceTiming) => void, private now = () => performance.now()) {
    this.started = now();
  }
  mark(stage: string, sentenceId?: string): void {
    try {
      this.sink({ traceId: this.id, stage, elapsedMs: Math.max(0, this.now() - this.started),
        ...(sentenceId ? { sentenceId } : {}) });
    } catch { /* Diagnostics must not interrupt listening or playback. */ }
  }
}

// Keep preparation timing at the promise boundary, including failed requests.
export async function prepareSpeech<T>(trace: VoiceLatencyTrace | undefined,
  sentenceId: string, prepare: () => Promise<T>): Promise<T> {
  trace?.mark("tts_requested", sentenceId);
  try {
    const result = await prepare();
    trace?.mark("tts_ready", sentenceId);
    return result;
  } catch (error) {
    trace?.mark("tts_failed", sentenceId);
    throw error;
  }
}
