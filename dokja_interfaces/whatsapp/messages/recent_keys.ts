// Remembers keys seen in the last few minutes. WhatsApp can deliver a message again after a
// reconnect, and the bot has to recognise the messages it sent itself when they come back.
export class RecentKeys {
  private readonly seenAt = new Map<string, number>();

  constructor(
    private readonly ttlMs = 10 * 60_000,
    private readonly maxKeys = 1000,
    private readonly now: () => number = Date.now,
  ) {}

  // Records the key without reporting whether it was known.
  add(key: string): void {
    this.seen(key);
  }

  // Whether the key is known, without recording it.
  has(key: string): boolean {
    this.expire();
    return this.seenAt.has(key);
  }

  // Returns true when the key was already seen, and records it otherwise.
  seen(key: string): boolean {
    this.expire();
    if (this.seenAt.has(key)) {
      return true;
    }
    this.seenAt.set(key, this.now());
    while (this.seenAt.size > this.maxKeys) {
      const oldest = this.seenAt.keys().next().value;
      if (oldest === undefined) {
        break;
      }
      this.seenAt.delete(oldest);
    }
    return false;
  }

  private expire(): void {
    const now = this.now();
    for (const [oldKey, at] of this.seenAt) {
      if (now - at <= this.ttlMs) {
        break; // entries are in insertion order, so the rest are newer
      }
      this.seenAt.delete(oldKey);
    }
  }
}
