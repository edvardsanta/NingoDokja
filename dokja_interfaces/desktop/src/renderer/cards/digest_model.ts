import { isRecord, text } from "../../shared/records.js";
import type { DigestItem, DigestPage, DigestSource, DigestStatus, Off } from "../../shared/replies.js";
import type { Tone } from "./health_model.js";

// The first page is short on purpose: offer, don't dump. "Show more" asks for this many more.
export const SHOW_STEP = 8;
// The most the digest hands over in one page.
export const MAX_SHOWN = 50;

const num = (value: unknown): number =>
  typeof value === "number" && Number.isFinite(value) ? value : 0;

function parseSource(value: unknown): DigestSource | undefined {
  if (!isRecord(value)) return undefined;
  const id = text(value.id);
  if (id === "") return undefined;
  return {
    id,
    name: text(value.name) || id,
    state: text(value.state) || "unknown",
    running: value.running === true,
    items: num(value.items),
    skipped: num(value.skipped),
    lastOk: text(value.lastOk),
    error: text(value.error),
  };
}

// The reply crossed a process boundary, so its shape is checked here too.
export function parseDigestStatus(value: unknown): DigestStatus | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (!Array.isArray(value.sources)) return undefined;
  return {
    configured: value.configured === true,
    directoryError: text(value.directoryError),
    ok: num(value.ok),
    failed: num(value.failed),
    pending: num(value.pending),
    disabled: num(value.disabled),
    invalid: num(value.invalid),
    items: num(value.items),
    sources: value.sources.map(parseSource).filter((source): source is DigestSource => source !== undefined),
  };
}

function parseItem(value: unknown): DigestItem | undefined {
  if (!isRecord(value)) return undefined;
  const title = text(value.title);
  if (title === "") return undefined;
  return {
    id: text(value.id),
    title,
    summary: text(value.summary),
    source: text(value.source),
    published: text(value.published),
  };
}

export function parseDigestPage(value: unknown): DigestPage | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (!Array.isArray(value.items)) return undefined;
  return {
    items: value.items.map(parseItem).filter((item): item is DigestItem => item !== undefined),
    total: num(value.total),
    offset: num(value.offset),
    more: num(value.more),
    updated: text(value.updated),
  };
}

// What the feeds service is doing, in the order the explanations matter: a directory that cannot
// be read, no directory at all, a directory with nothing in it, sources that are all off, and
// finally sources that run.
export type Following = "directory-error" | "not-configured" | "no-sources" | "none-enabled" | "following";

export function followingOf(status: DigestStatus): Following {
  if (status.directoryError !== "") return "directory-error";
  if (!status.configured) return "not-configured";
  if (status.sources.length === 0) return "no-sources";
  if (status.ok + status.failed + status.pending === 0) return "none-enabled";
  return "following";
}

export function sourceTone(state: string): Tone {
  switch (state) {
    case "ok":
      return "ok";
    case "failed":
    case "invalid":
      return "problem";
    case "pending":
      return "unchecked";
    default:
      return "off";
  }
}

// How many more items the button offers: one step, or what is left when that is less.
export function nextStep(page: DigestPage): number {
  return Math.min(page.more, SHOW_STEP);
}
