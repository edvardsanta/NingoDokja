import { isRecord, text } from "../../shared/records.js";
import type { Pulse } from "../pulse.js";
import type { CardState } from "./use_card.js";

// The services the TUI lists, in its order. Any other service the orchestrator reports comes after
// them, by name, so a service added later still shows up.
export const SERVICE_ORDER = ["meme", "chat_ai", "book", "knowledge", "memory", "scheduler"];

export type Tone = "ok" | "problem" | "off" | "unchecked" | "other";

export type ServiceRow = { name: string; status: string; detail: string; tone: Tone };

export type Health = { rows: ServiceRow[]; counts: Record<Tone, number> };

export function toneOf(status: string): Tone {
  switch (status) {
    case "ok":
      return "ok";
    case "disabled":
      return "off";
    case "error":
    case "stopped":
    case "degraded":
      return "problem";
    case "unchecked":
      return "unchecked";
    default:
      return "other"; // a status this screen does not know: shown as it came, never counted
  }
}

// The reply crossed a process boundary, so its shape is checked here too.
export function parseHealth(value: unknown): Health | undefined {
  if (!isRecord(value) || !isRecord(value.services)) return undefined;
  const { services } = value;

  const known = SERVICE_ORDER.filter((name) => name in services);
  const extra = Object.keys(services)
    .filter((name) => !SERVICE_ORDER.includes(name))
    .sort();

  const counts: Record<Tone, number> = { ok: 0, problem: 0, off: 0, unchecked: 0, other: 0 };
  const rows: ServiceRow[] = [];
  for (const name of [...known, ...extra]) {
    const entry = services[name];
    if (!isRecord(entry)) continue;
    const status = text(entry.status);
    const tone = toneOf(status);
    counts[tone] += 1;
    rows.push({ name, status, detail: text(entry.detail), tone });
  }
  return { rows, counts };
}

// What the name on top of the screen shows: loading is tuning, no answer is lost, and an answer
// is calm unless some service has a problem.
export function pulseOf(state: CardState<Health>): Pulse {
  switch (state.phase) {
    case "loading":
      return "tuning";
    case "error":
      return "lost";
    case "ready":
      return state.data.counts.problem > 0 ? "unwell" : "calm";
  }
}
