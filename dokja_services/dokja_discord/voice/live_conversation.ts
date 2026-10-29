// Continuous capture with one response/playback per room. Speech heard while
// Ningo answers is collected for the next turn; it never cuts off his answer.
export type VoiceTurn = { userId: string; userName?: string; text: string; sequence: number };
export type VoiceTurnContext = {
  userId: string;
  userName?: string;
  channelId: string;
  turns: VoiceTurn[];
  signal: AbortSignal;
  decide?(respond: boolean): void;
  waitForFloor?(): Promise<void>;
};

export class LiveConversation {
  private active = new Set<number>();
  private voiced = new Set<number>();
  private recognizing = new Set<number>();
  private pending = new Map<number, VoiceTurn>();
  private next = 0;
  private stopped = false;
  private timer?: ReturnType<typeof setTimeout>;
  private request?: AbortController;
  private floorWaiters = new Set<() => void>();

  constructor(private options: {
    channelId: string;
    interrupt(): void;
    onTurn(text: string, context: VoiceTurnContext): Promise<void>;
    onError(error: unknown): void;
    settleMs?: number;
  }) {}

  begin(holdFloor = true): number {
    const sequence = this.next++;
    if (this.stopped) return sequence;
    this.active.add(sequence);
    if (holdFloor) this.voiceActivity(sequence);
    return sequence;
  }

  voiceActivity(sequence: number): void {
    if (this.stopped || !this.active.has(sequence)) return;
    this.voiced.add(sequence);
    clearTimeout(this.timer);
  }

  // Recognition can finish out of order. Hold dispatch until captured segments
  // finish decoding, without treating decoding work as speech on the floor.
  transcribing(sequence: number): void {
    if (this.stopped || !this.active.has(sequence)) return;
    this.recognizing.add(sequence);
    clearTimeout(this.timer);
  }

  finish(sequence: number, turn?: Omit<VoiceTurn, "sequence">): void {
    if (this.stopped || !this.active.delete(sequence)) return;
    this.voiced.delete(sequence);
    this.recognizing.delete(sequence);
    if (turn?.text.trim()) {
      if (this.pending.size >= 32) {
        this.pending.delete(Math.min(...this.pending.keys()));
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

  private schedule(): void {
    if (this.stopped || this.request || this.voiced.size || this.recognizing.size || !this.pending.size) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { void this.flush(); }, this.options.settleMs ?? 80);
  }

  private async flush(): Promise<void> {
    if (this.stopped || this.request || this.voiced.size || this.recognizing.size || !this.pending.size) return;
    const turns = [...this.pending.values()].sort((a, b) => a.sequence - b.sequence);
    this.pending.clear();
    const last = turns[turns.length - 1];
    const controller = new AbortController();
    this.request = controller;
    let floorGranted = false;
    try {
      await this.options.onTurn(last.text, {
        userId: last.userId, userName: last.userName, channelId: this.options.channelId,
        turns, signal: controller.signal,
        // Eligibility affects whether the server replies, never current playback.
        decide() {},
        waitForFloor: async () => {
          if (floorGranted) { controller.signal.throwIfAborted(); return; }
          await this.waitForQuiet(controller.signal);
          floorGranted = true;
        },
      });
    } catch (error) {
      if (!controller.signal.aborted) this.options.onError(error);
    } finally {
      if (this.request === controller) this.request = undefined;
      this.schedule();
    }
  }
}
