// Keeps what the bot sent for a few minutes. When a device cannot decrypt a message it asks for it
// again, and WhatsApp needs the content to encrypt it a second time; without it the recipient is
// left on "waiting for this message".
export class SentMessages<T> {
  private readonly entries = new Map<string, { value: T; at: number }>();

  constructor(
    private readonly ttlMs = 10 * 60_000,
    private readonly maxEntries = 500,
    private readonly now: () => number = Date.now,
  ) {}

  set(id: string, value: T): void {
    this.expire();
    this.entries.delete(id);
    this.entries.set(id, { value, at: this.now() });
    while (this.entries.size > this.maxEntries) {
      const oldest = this.entries.keys().next().value;
      if (oldest === undefined) {
        break;
      }
      this.entries.delete(oldest);
    }
  }

  get(id: string): T | undefined {
    this.expire();
    return this.entries.get(id)?.value;
  }

  private expire(): void {
    const now = this.now();
    for (const [id, entry] of this.entries) {
      if (now - entry.at <= this.ttlMs) {
        break; // entries are in insertion order, so the rest are newer
      }
      this.entries.delete(id);
    }
  }
}
