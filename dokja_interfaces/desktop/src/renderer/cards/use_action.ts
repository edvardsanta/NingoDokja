import { useCallback, useEffect, useRef, useState } from "react";

import type { TransportError } from "../../shared/transport.js";
import type { LoadResult } from "./use_card.js";

export type ActionState<T> =
  | { phase: "idle" }
  | { phase: "loading" }
  | { phase: "ready"; data: T }
  | { phase: "error"; error: TransportError };

// Runs a request when the screen asks for one (a search, say) and keeps the latest answer. An
// answer that arrives after a newer request started, or after the card went away, is dropped.
// `run` must be stable (wrap it in useCallback).
export function useAction<A, T>(run: (argument: A) => Promise<LoadResult<T>>) {
  const [state, setState] = useState<ActionState<T>>({ phase: "idle" });
  const latest = useRef(0);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const start = useCallback(
    (argument: A) => {
      const turn = ++latest.current;
      const settle = (next: ActionState<T>) => {
        if (mounted.current && latest.current === turn) setState(next);
      };
      setState({ phase: "loading" });
      run(argument).then(
        (result) =>
          settle(
            result.ok
              ? { phase: "ready", data: result.data }
              : { phase: "error", error: result.error },
          ),
        () => settle({ phase: "error", error: { code: "unavailable", message: "request failed" } }),
      );
    },
    [run],
  );

  return { state, start };
}
