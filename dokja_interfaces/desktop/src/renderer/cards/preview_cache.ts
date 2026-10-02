// The latest previews, so paging back does not fetch the same files again. A video is a few
// megabytes as a data URL, so the cache is bounded by size as well as by count.
export class PreviewCache {
  private readonly entries = new Map<string, string>();
  private size = 0;

  constructor(
    private readonly maxEntries: number,
    private readonly maxChars: number,
  ) {}

  get(url: string): string | undefined {
    return this.entries.get(url);
  }

  remember(url: string, dataUrl: string): void {
    // One file bigger than the whole budget is not kept at all.
    if (dataUrl.length > this.maxChars) return;
    this.forget(url);
    this.entries.set(url, dataUrl);
    this.size += dataUrl.length;
    for (const [oldest, value] of this.entries) {
      if (this.entries.size <= this.maxEntries && this.size <= this.maxChars) break;
      this.entries.delete(oldest);
      this.size -= value.length;
    }
  }

  clear(): void {
    this.entries.clear();
    this.size = 0;
  }

  private forget(url: string): void {
    const old = this.entries.get(url);
    if (old === undefined) return;
    this.entries.delete(url);
    this.size -= old.length;
  }
}
