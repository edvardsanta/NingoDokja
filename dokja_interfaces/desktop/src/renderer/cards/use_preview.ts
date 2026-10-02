import { useEffect, useState } from "react";

import type { Transport, TransportError } from "../../shared/transport.js";
import { PreviewCache } from "./preview_cache.js";

export type PreviewState =
  | { phase: "loading" }
  | { phase: "ready"; dataUrl: string }
  | { phase: "failed"; error: TransportError };

// A page holds twelve memes: keep about two pages, within 96 million characters of data URL
// (about 70 MB of files).
const cache = new PreviewCache(24, 96_000_000);

export function clearPreviewCache(): void {
  cache.clear();
}

// Asks the shell for the picture or video at an address. `enabled` is false for what has none, so no
// request is made. An answer that arrives after the address changed is dropped.
export function usePreview(transport: Transport, url: string, enabled: boolean): PreviewState {
  const [state, setState] = useState<PreviewState>({ phase: "loading" });

  useEffect(() => {
    if (!enabled) return;
    const cached = cache.get(url);
    if (cached) {
      setState({ phase: "ready", dataUrl: cached });
      return;
    }

    let current = true;
    setState({ phase: "loading" });
    transport.preview(url).then(
      (result) => {
        if (!current) return;
        if (result.ok) {
          cache.remember(url, result.dataUrl);
          setState({ phase: "ready", dataUrl: result.dataUrl });
        } else {
          setState({ phase: "failed", error: result.error });
        }
      },
      () => {
        if (current) setState({ phase: "failed", error: { code: "unavailable", message: "request failed" } });
      },
    );
    return () => {
      current = false;
    };
  }, [transport, url, enabled]);

  return state;
}
