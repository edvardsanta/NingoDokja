import { isRecord, text } from "../../shared/records.js";
import type { MemeItem, MemePage, MemeStatus, Off } from "../../shared/replies.js";

export type Scope = "unsent" | "sent";

export const PAGE_SIZE = 12;

const num = (value: unknown): number =>
  typeof value === "number" && Number.isFinite(value) ? value : 0;

// The reply crossed a process boundary, so its shape is checked here too.
export function parseMemeStatus(value: unknown): MemeStatus | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (typeof value.unsent !== "number") return undefined;
  return { status: text(value.status) || "ok", unsent: num(value.unsent), sent: num(value.sent) };
}

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

const VIDEO_EXTENSIONS = [".mp4", ".webm", ".mov", ".mkv", ".m4v", ".gifv"];

// A video has no preview: the shell only fetches pictures.
export function isVideoAddress(url: string): boolean {
  try {
    const path = new URL(url).pathname.toLowerCase();
    return VIDEO_EXTENSIONS.some((extension) => path.endsWith(extension));
  } catch {
    return false;
  }
}

// The first to the last meme of a page, counting from 1.
export function pageRange(page: MemePage): { from: number; to: number } {
  if (page.memes.length === 0) return { from: 0, to: 0 };
  return { from: page.offset + 1, to: page.offset + page.memes.length };
}

// "2026-09-03T10:00:00" is "2026-09-03".
export function shortDate(iso: string): string {
  return /^\d{4}-\d{2}-\d{2}/.exec(iso)?.[0] ?? iso;
}
