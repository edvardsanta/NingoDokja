import { isRecord, text } from "../../shared/records.js";
import type { MemeItem, MemePage, Off } from "../../shared/replies.js";

const num = (value: unknown): number =>
  typeof value === "number" && Number.isFinite(value) ? value : 0;

function parseMeme(value: unknown): MemeItem | undefined {
  if (!isRecord(value)) return undefined;
  return {
    url: text(value.url),
    title: text(value.title),
    tags: text(value.tags),
    source: text(value.source),
    createdAt: text(value.createdAt),
    sentAt: text(value.sentAt),
  };
}

// The reply crossed a process boundary, so its shape is checked here too.
export function parseMemePage(value: unknown): MemePage | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (!Array.isArray(value.memes)) return undefined;
  return {
    total: num(value.total),
    offset: num(value.offset),
    memes: value.memes.map(parseMeme).filter((meme): meme is MemeItem => meme !== undefined),
  };
}
