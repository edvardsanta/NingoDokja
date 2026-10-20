import { createContext, useContext, useMemo, useState, type ReactNode } from "react";

import type { Tone } from "./cards/health_model.js";

// How the system feels, as the name on top of the screen shows it: tuning while something loads,
// calm when everything answers, unwell when a service is down, lost when nothing answers.
export type Pulse = "tuning" | "calm" | "unwell" | "lost";

// The tone of each service and interface by name, from the last status the health card read. It is
// empty until one arrives, and the sidebar shows a dot only for a name it finds here: the sidebar
// never asks a service itself.
export type Tones = Readonly<Record<string, Tone>>;

type PulseValue = {
  pulse: Pulse;
  tones: Tones;
  report: (pulse: Pulse) => void;
  reportTones: (tones: Tones) => void;
};

const NO_TONES: Tones = {};

const PulseContext = createContext<PulseValue>({
  pulse: "tuning",
  tones: NO_TONES,
  report: () => {},
  reportTones: () => {},
});

export function PulseProvider({ children }: { children: ReactNode }) {
  const [pulse, setPulse] = useState<Pulse>("tuning");
  const [tones, setTones] = useState<Tones>(NO_TONES);
  const value = useMemo(() => ({ pulse, tones, report: setPulse, reportTones: setTones }), [pulse, tones]);
  return <PulseContext.Provider value={value}>{children}</PulseContext.Provider>;
}

export function usePulse(): PulseValue {
  return useContext(PulseContext);
}
