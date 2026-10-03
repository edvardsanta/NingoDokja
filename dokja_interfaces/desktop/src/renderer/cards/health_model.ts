import { isRecord, text } from "../../shared/records.js";
import type { JobState } from "../../shared/replies.js";
import type { Pulse } from "../pulse.js";
import type { CardState } from "./use_card.js";

// The services the TUI lists, in its order. Any other service the orchestrator reports comes after
// them, by name, so a service added later still shows up.
export const SERVICE_ORDER = ["meme", "chat_ai", "book", "knowledge", "memory", "scheduler"];

export type Tone = "ok" | "problem" | "off" | "unchecked" | "other";

// `enabled` is there when the orchestrator has a switch for the service, and only then.
export type ServiceRow = { name: string; status: string; detail: string; tone: Tone; enabled?: boolean };

export type Health = { rows: ServiceRow[]; counts: Record<Tone, number>; jobs: JobState[] };

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
    rows.push({ name, status, detail: text(entry.detail), tone, ...(typeof entry.enabled === "boolean" ? { enabled: entry.enabled } : {}) });
  }
  const jobs = Array.isArray(value.jobs) ? value.jobs.map(parseJob).filter((job): job is JobState => job !== undefined) : [];
  return { rows, counts, jobs };
}

function parseJob(value: unknown): JobState | undefined {
  if (!isRecord(value) || typeof value.name !== "string" || value.name === "" || typeof value.enabled !== "boolean") return undefined;
  return {
    name: value.name,
    enabled: value.enabled,
    interval: text(value.interval),
    intervalOverride: value.intervalOverride === true,
    nextAt: text(value.nextAt),
    lastAt: text(value.lastAt),
    lastOutcome: text(value.lastOutcome),
    lastError: text(value.lastError),
  };
}

// The orchestrator reports a Go duration ("6h0m0s"). Whole parts only, zeros dropped: "6h", "1h 30m".
function durationSeconds(interval: string): number | undefined {
  const match = /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?$/.exec(interval);
  if (!match || interval === "") return undefined;
  return Number(match[1] ?? 0) * 3600 + Number(match[2] ?? 0) * 60 + Number(match[3] ?? 0);
}

export function formatInterval(interval: string): string {
  const total = durationSeconds(interval);
  if (!total) return interval;
  const parts = [
    ["h", Math.floor(total / 3600)],
    ["m", Math.floor((total % 3600) / 60)],
    ["s", total % 60],
  ] as const;
  return parts.filter(([, amount]) => amount > 0).map(([unit, amount]) => `${amount}${unit}`).join(" ");
}

export type IntervalUnit = "m" | "h";

// What the interval editor starts from: whole hours in hours, anything else in minutes.
export function intervalFields(interval: string): { amount: string; unit: IntervalUnit } {
  const total = durationSeconds(interval);
  if (!total) return { amount: "", unit: "m" };
  if (total % 3600 === 0) return { amount: String(total / 3600), unit: "h" };
  return { amount: String(Math.max(1, Math.round(total / 60))), unit: "m" };
}

// The orchestrator keeps a job between a minute and thirty days; the editor offers nothing else.
const MAX_INTERVAL_MINUTES = 30 * 24 * 60;

// The text the shell sends for the editor's fields, or undefined while they are not a valid interval.
export function intervalOf(amount: string, unit: IntervalUnit): string | undefined {
  if (!/^[0-9]{1,5}$/.test(amount)) return undefined;
  const minutes = Number(amount) * (unit === "h" ? 60 : 1);
  return minutes >= 1 && minutes <= MAX_INTERVAL_MINUTES ? `${Number(amount)}${unit}` : undefined;
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
