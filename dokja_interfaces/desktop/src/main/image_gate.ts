const CAPACITY = 500;
const MAX_URL_CHARS = 2048;

// The shell previews only images the orchestrator itself listed, so the screen cannot make it
// fetch an address of its own. The gate remembers the latest addresses and forgets the oldest.
export class ImageGate {
  private readonly urls = new Set<string>();

  remember(urls: string[]): void {
    for (const url of urls) {
      if (typeof url !== "string" || url === "" || url.length > MAX_URL_CHARS) continue;
      this.urls.delete(url);
      this.urls.add(url);
    }
    for (const oldest of this.urls) {
      if (this.urls.size <= CAPACITY) break;
      this.urls.delete(oldest);
    }
  }

  has(url: string): boolean {
    return this.urls.has(url);
  }
}
