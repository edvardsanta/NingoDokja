import { VoiceLatencyTrace, type VoiceTiming } from "./latency_trace.js";
// Continuous capture with one response/playback per room. Speech heard while
// Ningo answers is collected for the next turn; it never cuts off his answer.
// One exception, before the answer has begun: an answer made for what was said is
// worth less once the room has moved on, so it is not spoken. See movedOn().
export type VoiceTurn = { userId: string; userName?: string; text: string; sequence: number };
export type VoiceTurnContext = {
  userId: string;
  userName?: string;
  channelId: string;
  turns: VoiceTurn[];
  signal: AbortSignal;
  trace?: VoiceLatencyTrace;
  decide?(respond: boolean): void;
  waitForFloor?(): Promise<void>;
};

// A reply is made for the turns pending when it was dispatched, and can be spoken seconds later,
// after the floor has been waited for. If the room said something that matters in between, the
// reply answers a point people have left ("it went back", in the owner's words). Filler that
// recognition invents ("Obrigado.") or a stray word is not something that matters.
const MIN_WORDS_OF_A_TURN_THAT_MATTERS = 3;
const MIN_WORDS_THAT_MOVE_THE_ROOM_ON = 6;
// How many times a batch may be asked again before its answer is spoken whatever the room did.
// Without a bound, a room that never stops talking would never be answered.
const MAX_REDISPATCHES = 2;
const wordsIn = (text: string): number => text.split(/\s+/).filter(Boolean).length;

export class LiveConversation {
  private active = new Set<number>();
  private voiced = new Set<number>();
  private recognizing = new Set<number>();
  private pending = new Map<number, VoiceTurn>();
  private traces = new Map<number, VoiceLatencyTrace>();
  private next = 0;
  private stopped = false;
  private timer?: ReturnType<typeof setTimeout>;
  private request?: AbortController;
  private floorWaiters = new Set<() => void>();
  // How many times the current batch has been asked again because the room moved on.
  private redispatches = 0;

  constructor(private options: {
    channelId: string;
    interrupt(): void;
    onTurn(text: string, context: VoiceTurnContext): Promise<void>;
    onError(error: unknown): void;
    settleMs?: number;
    onTiming?(event: VoiceTiming): void;
  }) {}

  begin(holdFloor = true): number {
    const sequence = this.next++;
    if (this.stopped) return sequence;
    this.active.add(sequence);
    const trace = new VoiceLatencyTrace(event => this.options.onTiming?.(event));
    this.traces.set(sequence, trace);
    trace.mark("capture_started");
    if (holdFloor) this.voiceActivity(sequence);
    return sequence;
  }

  voiceActivity(sequence: number): void {
    if (this.stopped || !this.active.has(sequence)) return;
    this.voiced.add(sequence);
  }

  // Recognition can finish out of order. Hold dispatch until captured segments
  // finish decoding, without treating decoding work as speech on the floor.
  transcribing(sequence: number): void {
    if (this.stopped || !this.active.has(sequence)) return;
    this.traces.get(sequence)?.mark("recognition_queued");
    this.recognizing.add(sequence);
  }

  recognitionStarted(sequence: number): void {
    this.traces.get(sequence)?.mark("recognition_started");
  }

  finish(sequence: number, turn?: Omit<VoiceTurn, "sequence">, outcome?: "observed"): void {
    if (this.stopped || !this.active.delete(sequence)) return;
    this.traces.get(sequence)?.mark(outcome === "observed" ? "observation_published" : turn?.text.trim() ? "transcript_ready" : "segment_discarded");
    if (!turn?.text.trim()) this.traces.delete(sequence);
    this.voiced.delete(sequence);
    this.recognizing.delete(sequence);
    if (turn?.text.trim()) {
      if (this.pending.size >= 32) {
        const dropped = Math.min(...this.pending.keys());
        this.traces.get(dropped)?.mark("pending_overflow");
        this.traces.delete(dropped);
        this.pending.delete(dropped);
        this.options.onError(new Error("Voice conversation context capacity reached"));
      }
      this.pending.set(sequence, { ...turn, text: turn.text.trim(), sequence });
    }
    this.schedule();
    for (const wake of [...this.floorWaiters]) wake();
  }

  stop(): void {
    this.stopped = true;
    clearTimeout(this.timer);
    this.request?.abort();
    for (const trace of this.traces.values()) trace.mark("listener_stopped");
    this.traces.clear();
    this.pending.clear();
    this.active.clear();
    this.voiced.clear();
    this.recognizing.clear();
    for (const wake of [...this.floorWaiters]) wake();
    this.floorWaiters.clear();
    this.options.interrupt();
  }

  private waitForQuiet(signal: AbortSignal): Promise<void> {
    signal.throwIfAborted();
    if (!this.voiced.size) return Promise.resolve();
    return new Promise((resolve, reject) => {
      const wake = () => {
        if (!signal.aborted && !this.stopped && this.voiced.size) return;
        this.floorWaiters.delete(wake);
        signal.removeEventListener("abort", wake);
        if (signal.aborted || this.stopped) reject(signal.reason ?? new Error("Voice session stopped"));
        else resolve();
      };
      this.floorWaiters.add(wake);
      signal.addEventListener("abort", wake, { once: true });
    });
  }

  // Later speech can overlap preparation. Earlier unfinished captures must still
  // settle so recognition completion never reverses the conversation order.
  private waitingForEarlierCapture(): boolean {
    const boundary = Math.max(...this.pending.keys());
    return [...this.active].some(sequence => sequence < boundary);
  }

  // The room has moved on when turns captured after the batch's last one, and already transcribed,
  // say enough to matter. They wait in `pending` for the next dispatch.
  private movedOn(afterSequence: number): boolean {
    const newer = [...this.pending.values()].filter(turn => turn.sequence > afterSequence);
    const words = newer.map(turn => wordsIn(turn.text)).filter(count => count >= MIN_WORDS_OF_A_TURN_THAT_MATTERS);
    return words.reduce((total, count) => total + count, 0) >= MIN_WORDS_THAT_MOVE_THE_ROOM_ON;
  }

  private schedule(): void {
    if (this.stopped || this.request || !this.pending.size || this.waitingForEarlierCapture()) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { void this.flush(); }, this.options.settleMs ?? 80);
  }

  private async flush(): Promise<void> {
    if (this.stopped || this.request || !this.pending.size || this.waitingForEarlierCapture()) return;
    const turns = [...this.pending.values()].sort((a, b) => a.sequence - b.sequence);
    this.pending.clear();
    const last = turns[turns.length - 1];
    const trace = this.traces.get(last.sequence);
    for (const turn of turns) {
      this.traces.get(turn.sequence)?.mark("response_dispatched");
      this.traces.delete(turn.sequence);
    }
    const controller = new AbortController();
    this.request = controller;
    let floorGranted = false;
    let askedAgain = false;
    try {
      await this.options.onTurn(last.text, {
        userId: last.userId, userName: last.userName, channelId: this.options.channelId,
        turns, signal: controller.signal, trace,
        // Eligibility affects whether the server replies, never current playback.
        decide() {},
        waitForFloor: async () => {
          if (floorGranted) { controller.signal.throwIfAborted(); return; }
          trace?.mark("floor_wait_started");
          await this.waitForQuiet(controller.signal);
          // Nothing has been spoken yet, so there is no answer to cut off: this is the one moment
          // an answer for an earlier point can be replaced. The old turns go back in front of the
          // new ones and the whole is asked again, so the reply covers what is being said now.
          if (this.redispatches < MAX_REDISPATCHES && this.movedOn(last.sequence)) {
            this.redispatches++;
            askedAgain = true;
            for (const turn of turns) this.pending.set(turn.sequence, turn);
            trace?.mark("response_stale");
            const moved = new Error("The room moved on before the answer was spoken");
            controller.abort(moved);
            throw moved;
          }
          trace?.mark("floor_granted");
          floorGranted = true;
          this.redispatches = 0;
        },
      });
    } catch (error) {
      trace?.mark(controller.signal.aborted ? "response_cancelled" : "response_failed");
      if (!controller.signal.aborted) this.options.onError(error);
    } finally {
      trace?.mark("response_handler_finished");
      if (!askedAgain) this.redispatches = 0;
      if (this.request === controller) this.request = undefined;
      this.schedule();
    }
  }
}
