import type { PreviewResult, TransportErrorCode } from "../shared/transport.js";
import type { ImageGate } from "./image_gate.js";
import { ImageError } from "./image_fetch.js";

type Deps = {
  gate: Pick<ImageGate, "has">;
  fetchImage: (url: string) => Promise<string>;
  // How many images are fetched at once, so a page of memes does not hammer a host.
  concurrency?: number;
};

const failure = (code: TransportErrorCode, message: string): PreviewResult => ({
  ok: false,
  error: { code, message },
});

// Answers a screen's request for a preview: only an address the orchestrator listed, a few at a
// time, and the same address asked twice at once is fetched once.
export function createPreviewer({ gate, fetchImage, concurrency = 3 }: Deps) {
  const inFlight = new Map<string, Promise<PreviewResult>>();
  const waiting: Array<() => void> = [];
  let running = 0;

  const acquire = async (): Promise<void> => {
    if (running >= concurrency) await new Promise<void>((resume) => waiting.push(resume));
    running += 1;
  };
  const release = (): void => {
    running -= 1;
    waiting.shift()?.();
  };

  const run = async (url: string): Promise<PreviewResult> => {
    await acquire();
    try {
      return { ok: true, dataUrl: await fetchImage(url) };
    } catch (error) {
      return error instanceof ImageError
        ? failure(error.code, error.message)
        : failure("unavailable", "the image could not be fetched");
    } finally {
      release();
    }
  };

  return (url: unknown): Promise<PreviewResult> => {
    if (typeof url !== "string" || !gate.has(url)) {
      return Promise.resolve(failure("denied", "that image is not one the orchestrator listed"));
    }
    const pending = inFlight.get(url);
    if (pending) return pending;

    const started = run(url).finally(() => inFlight.delete(url));
    inFlight.set(url, started);
    return started;
  };
}
