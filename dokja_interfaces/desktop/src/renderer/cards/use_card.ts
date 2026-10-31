import { useCallback, useEffect, useRef, useState } from "react";

import type { TransportError } from "../../shared/transport.js";

export type LoadResult<T> = { ok: true; data: T } | { ok: false; error: TransportError };

export type CardState<T> =
  | { phase: "loading" }
  | { phase: "ready"; data: T }
  | { phase: "error"; error: TransportError };

// Loads a card's data when it appears and again on reload(). `load` must be stable (wrap it in
// useCallback), or the card reloads on every render. An answer that arrives after the card went
// away, or after a newer load started, is dropped. poll() reads again but keeps the data on screen
// until the answer arrives, so a card that watches a slow operation does not blink "loading"; while
// it waits, `refreshing` is true, so whatever holds a refresh can hold it for a quiet read too.
export function useCard<T>(load: () => Promise<LoadResult<T>>) {
  const [state, setState] = useState<CardState<T>>({ phase: "loading" });
  const [round, setRound] = useState(0);
  const [refreshing, setRefreshing] = useState(false);
  const quiet = useRef(false);

  useEffect(() => {
    let current = true;
    if (!quiet.current) setState({ phase: "loading" });
    quiet.current = false;
    load().then(
      (result) => {
        if (!current) return;
        setRefreshing(false);
        setState(
          result.ok
            ? { phase: "ready", data: result.data }
            : { phase: "error", error: result.error },
        );
      },
      () => {
        if (!current) return;
        setRefreshing(false);
        setState({ phase: "error", error: { code: "unavailable", message: "request failed" } });
      },
    );
    return () => {
      current = false;
    };
  }, [load, round]);

  const reload = useCallback(() => setRound((value) => value + 1), []);
  const poll = useCallback(() => {
    quiet.current = true;
    setRefreshing(true);
    setRound((value) => value + 1);
  }, []);
  return { state, reload, poll, refreshing };
}
