// Audio transport scheduling: capture each speaker independently, then yield the floor
// only when all capture/transcription work has finished. Application policy stays in domains.
export type VoiceTurn = { userId: string; userName?: string; text: string; sequence: number };
export type VoiceTurnContext = {
  userId: string; userName?: string; channelId: string;
  turns: VoiceTurn[]; signal: AbortSignal;
};
export class LiveConversation {
  private active = new Set<number>();
  private pending = new Map<number, VoiceTurn>();
  private next = 0;
  private revision = 0;
  private stopped = false;
  private timer?: ReturnType<typeof setTimeout>;
  private reply?: AbortController;
  private requests = new Set<AbortController>();
  constructor(private options: {
    channelId: string;
    interrupt(): void;
    onTurn(text: string, context: VoiceTurnContext): Promise<void>;
    onError(error: unknown): void;
    settleMs?: number;
  }) {}
  begin(): number {
    const sequence = this.next++;
    if (this.stopped) return sequence;
    this.active.add(sequence);
    this.revision++;
    clearTimeout(this.timer);
    this.reply?.abort();
    this.options.interrupt();
    return sequence;
  }
  // Each outstanding transcription remains part of the floor until it finishes.
  finish(sequence: number, turn?: Omit<VoiceTurn, "sequence">): void {
    if (this.stopped || !this.active.delete(sequence)) return;
    if (turn?.text.trim()) {
      if (this.pending.size >= 32) {
        const oldest = Math.min(...this.pending.keys());
        this.pending.delete(oldest);
        this.options.onError(new Error("Voice conversation context capacity reached"));
      }
      this.pending.set(sequence, { ...turn, text: turn.text.trim(), sequence });
    }
    this.schedule();
  }
  stop(): void {
    this.stopped = true;
    clearTimeout(this.timer);
    for (const request of this.requests) request.abort();
    this.reply?.abort();
    this.pending.clear();
    this.active.clear();
    this.options.interrupt();
  }
  private schedule(): void {
    if (this.stopped || this.active.size || !this.pending.size) return;
    clearTimeout(this.timer);
    this.timer = setTimeout(() => { void this.flush(); }, this.options.settleMs ?? 80);
  }
  private async flush(): Promise<void> {
    if (this.stopped || this.active.size || !this.pending.size) return;
    const turns = [...this.pending.values()].sort((a, b) => a.sequence - b.sequence);
    this.pending.clear();
    const last = turns[turns.length - 1];
    const controller = new AbortController();
    const revision = this.revision;
    this.reply = controller;
    this.requests.add(controller);
    try {
      await this.options.onTurn(last.text, {
        userId: last.userId, userName: last.userName, channelId: this.options.channelId,
        turns, signal: controller.signal,
      });
    } catch (error) {
      if (!controller.signal.aborted) this.options.onError(error);
    } finally {
      this.requests.delete(controller);
      if (this.reply === controller && this.revision === revision) this.reply = undefined;
      this.schedule();
    }
  }
}
