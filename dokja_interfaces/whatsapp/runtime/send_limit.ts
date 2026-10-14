// A fixed cap on what the desktop may send through the linked account. A burst of automated
// messages is what gets a number restricted, so this is not a setting: it is part of the feature.
export type Window = { count: number; windowMs: number };

export const sendLimits: Window[] = [
  { count: 6, windowMs: 60_000 },
  { count: 40, windowMs: 60 * 60_000 },
];

export class SendLimit {
  private readonly taken: number[] = [];

  constructor(
    private readonly windows: Window[] = sendLimits,
    private readonly now: () => number = Date.now,
  ) {}

  // Takes one message from the budget. Returns false, and takes nothing, when any window is full.
  take(): boolean {
    const now = this.now();
    const longest = Math.max(...this.windows.map((window) => window.windowMs));
    while (this.taken.length > 0 && now - this.taken[0] > longest) {
      this.taken.shift();
    }
    for (const window of this.windows) {
      const recent = this.taken.filter((at) => now - at <= window.windowMs).length;
      if (recent >= window.count) {
        return false;
      }
    }
    this.taken.push(now);
    return true;
  }
}
