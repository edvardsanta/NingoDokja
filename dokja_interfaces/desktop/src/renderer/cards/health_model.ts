import { isRecord, text } from "../../shared/records.js";
import type { JobState } from "../../shared/replies.js";
import type { Pulse } from "../pulse.js";
import type { CardState } from "./use_card.js";

// The services the TUI lists, in its order. Any other service the orchestrator reports comes after
// them, by name, so a service added later still shows up.
export const SERVICE_ORDER = ["meme", "chat", "book", "knowledge", "memory", "scheduler"];

export type Tone = "ok" | "problem" | "off" | "unchecked" | "other";

// `enabled` is there when the orchestrator has a switch for the service, and only then.
// `kind` is "interface" for Discord and WhatsApp, which are listed apart from the services,
// and `state` is the name of the state they report.
export type ServiceRow = {
  name: string;
  status: string;
  detail: string;
  tone: Tone;
  enabled?: boolean;
  kind?: "interface";
  configured?: boolean;
  state?: string;
};

// `rows` are the services and `interfaces` the chat interfaces; `counts` covers both.
export type Health = { rows: ServiceRow[]; interfaces: ServiceRow[]; counts: Record<Tone, number>; jobs: JobState[] };

// The interfaces the desktop knows, in the order it lists them. Any other interface comes after them.
export const INTERFACE_ORDER = ["discord", "whatsapp"];

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

  const isInterface = (name: string) => isRecord(services[name]) && services[name].kind === "interface";
  const names = Object.keys(services).filter((name) => isRecord(services[name]));
  const known = SERVICE_ORDER.filter((name) => names.includes(name) && !isInterface(name));
  const extra = names.filter((name) => !SERVICE_ORDER.includes(name) && !isInterface(name)).sort();
  const interfaceNames = names.filter(isInterface);
  const interfaceKnown = INTERFACE_ORDER.filter((name) => interfaceNames.includes(name));
  const interfaceExtra = interfaceNames.filter((name) => !INTERFACE_ORDER.includes(name)).sort();

  const counts: Record<Tone, number> = { ok: 0, problem: 0, off: 0, unchecked: 0, other: 0 };
  const rowOf = (name: string): ServiceRow => {
    const entry = services[name] as Record<string, unknown>;
    const status = text(entry.status);
    const tone = toneOf(status);
    counts[tone] += 1;
    return {
      name,
      status,
      detail: text(entry.detail),
      tone,
      ...(typeof entry.enabled === "boolean" ? { enabled: entry.enabled } : {}),
      ...(entry.kind === "interface" ? { kind: "interface" as const } : {}),
      ...(entry.kind === "interface" && typeof entry.configured === "boolean" ? { configured:entry.configured } : {}),
      ...(entry.kind === "interface" && text(entry.state) ? { state: text(entry.state) } : {}),
    };
  };
  const rows = [...known, ...extra].map(rowOf);
  const interfaces = [...interfaceKnown, ...interfaceExtra].map(rowOf);
  const jobs = Array.isArray(value.jobs) ? value.jobs.map(parseJob).filter((job): job is JobState => job !== undefined) : [];
  return { rows, interfaces, counts, jobs };
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

// The link reply the shell projected. It crossed a process boundary, so its shape is checked here too:
// the picture is accepted only as the SVG data URL the shell builds.
export type LinkView = { state: string; image?: string };

export function parseLink(value: unknown): LinkView | undefined {
  if (!isRecord(value) || typeof value.state !== "string" || !/^[a-z][a-z_]{0,31}$/.test(value.state)) return undefined;
  const image = typeof value.image === "string" && value.image.startsWith("data:image/svg+xml;base64,") ? value.image : undefined;
  return { state: value.state, ...(image ? { image } : {}) };
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

// The tone of every service and interface by name, for the sidebar's dots. A status that has not
// arrived, or did not arrive, has no tones: the sidebar then shows no dot rather than a guess.
export function tonesOf(state: CardState<Health>): Record<string, Tone> {
  if (state.phase !== "ready") return {};
  return Object.fromEntries([...state.data.rows, ...state.data.interfaces].map((row) => [row.name, row.tone]));
}
