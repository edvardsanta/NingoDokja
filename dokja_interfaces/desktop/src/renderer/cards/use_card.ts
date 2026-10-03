import { useCallback, useEffect, useState } from "react";

import type { TransportError } from "../../shared/transport.js";

export type LoadResult<T> = { ok: true; data: T } | { ok: false; error: TransportError };

export type CardState<T> =
  | { phase: "loading" }
  | { phase: "ready"; data: T }
  | { phase: "error"; error: TransportError };

// Loads a card's data when it appears and again on reload(). `load` must be stable (wrap it in
// useCallback), or the card reloads on every render. An answer that arrives after the card went
// away, or after a newer load started, is dropped.
export function useCard<T>(load: () => Promise<LoadResult<T>>) {
  const [state, setState] = useState<CardState<T>>({ phase: "loading" });
  const [round, setRound] = useState(0);

  useEffect(() => {
    let current = true;
    setState({ phase: "loading" });
    load().then(
      (result) => {
        if (!current) return;
        setState(
          result.ok
            ? { phase: "ready", data: result.data }
            : { phase: "error", error: result.error },
        );
      },
      () => {
        if (!current) return;
        setState({ phase: "error", error: { code: "unavailable", message: "request failed" } });
      },
    );
    return () => {
      current = false;
    };
  }, [load, round]);

  const reload = useCallback(() => setRound((value) => value + 1), []);
  return { state, reload };
}
