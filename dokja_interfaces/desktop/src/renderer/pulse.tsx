import { createContext, useContext, useMemo, useState, type ReactNode } from "react";

// How the system feels, as the name on top of the screen shows it: tuning while something loads,
// calm when everything answers, unwell when a service is down, lost when nothing answers.
export type Pulse = "tuning" | "calm" | "unwell" | "lost";

type PulseValue = { pulse: Pulse; report: (pulse: Pulse) => void };

const PulseContext = createContext<PulseValue>({ pulse: "tuning", report: () => {} });

export function PulseProvider({ children }: { children: ReactNode }) {
  const [pulse, setPulse] = useState<Pulse>("tuning");
  const value = useMemo(() => ({ pulse, report: setPulse }), [pulse]);
  return <PulseContext.Provider value={value}>{children}</PulseContext.Provider>;
}

export function usePulse(): PulseValue {
  return useContext(PulseContext);
}
