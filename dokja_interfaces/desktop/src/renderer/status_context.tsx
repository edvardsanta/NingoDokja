import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import type { Transport } from "../shared/transport.js";
import { parseHealth, pulseOf, tonesOf, type Health, type Tone } from "./cards/health_model.js";
import { useCard, type CardState, type LoadResult } from "./cards/use_card.js";

// How the system feels, as the name on top of the screen shows it: tuning while something loads,
// calm when everything answers, unwell when a service is down, lost when nothing answers.
export type Pulse = "tuning" | "calm" | "unwell" | "lost";

// The tone of each service and interface by name, from the last status that arrived. It is empty
// until one arrives, and the sidebar shows a dot only for a name it finds here.
export type Tones = Readonly<Record<string, Tone>>;

// Longer than the few seconds the orchestrator takes to give up on a stopped service.
const TIMEOUT_MS = 15_000;

export async function loadHealth(transport: Transport): Promise<LoadResult<Health>> {
  const result = await transport.request("ningo.status", {}, { timeoutMs: TIMEOUT_MS });
  if (!result.ok) return result;
  const health = parseHealth(result.result);
  return health
    ? { ok: true, data: health }
    : { ok: false, error: { code: "unexpected", message: "unexpected status reply" } };
}

type StatusValue = {
  state: CardState<Health>;
  reload: () => void;
  pulse: Pulse;
  tones: Tones;
};

const NO_TONES: Tones = {};

const StatusContext = createContext<StatusValue>({
  state: { phase: "loading" },
  reload: () => {},
  pulse: "tuning",
  tones: NO_TONES,
});

// The one `ningo.status` the screen reads, at the top of it: the name's pulse, the sidebar's dots
// and the System workspace all show this answer, so the screen can open on any workspace and still
// know how the system is. It is read when the screen starts and again when System asks; no
// workspace asks its own service for the dots.
export function StatusProvider({ transport, children }: { transport: Transport; children: ReactNode }) {
  const load = useCallback(() => loadHealth(transport), [transport]);
  const { state, reload } = useCard(load);
  const [tones, setTones] = useState<Tones>(NO_TONES);

  // while a reload runs, the sidebar keeps the tones it has instead of blinking them away
  useEffect(() => {
    if (state.phase !== "loading") setTones(tonesOf(state));
  }, [state]);

  const value = useMemo(() => ({ state, reload, pulse: pulseOf(state), tones }), [state, reload, tones]);
  return <StatusContext.Provider value={value}>{children}</StatusContext.Provider>;
}

export function useStatus(): StatusValue {
  return useContext(StatusContext);
}
