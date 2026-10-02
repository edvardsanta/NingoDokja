import { INGEST_LIMITS as LIMITS, base64Length, type IngestMode } from "../../shared/ingest.js";
import { isRecord, text } from "../../shared/records.js";
import type { IngestResult, Off } from "../../shared/replies.js";
import type { MessageId } from "../i18n/i18n.js";

// A file the person chose, already read: it is read when chosen, not when sent, so the click that
// sends it is the click the shell sees.
export type FileDraft = { filename: string; content: string; bytes: number };

export type Draft = {
  mode: IngestMode;
  title: string;
  body: string;
  address: string;
  tags: string;
  file?: FileDraft;
};

// What the screen says when it cannot send: a message and the numbers it needs.
export type Problem = { id: MessageId; params?: Record<string, number> };

export type Checked = { ok: true; payload: Record<string, unknown> } | { ok: false; problem: Problem };

const refuse = (id: MessageId, params?: Record<string, number>): Checked => ({
  ok: false,
  problem: params ? { id, params } : { id },
});

export const FILE_MEGABYTES = LIMITS.fileBytes / (1024 * 1024);

function tagsOf(raw: string): string[] {
  const tags: string[] = [];
  for (const item of raw.split(",")) {
    const tag = item.trim();
    if (tag !== "" && !tags.includes(tag)) tags.push(tag);
  }
  return tags;
}

function isWebAddress(raw: string): boolean {
  try {
    const url = new URL(raw);
    return (url.protocol === "http:" || url.protocol === "https:") && url.hostname !== "";
  } catch {
    return false;
  }
}

// The same rules the shell applies, so the screen can say what is wrong before anything is sent.
// The shell stays the one that decides.
export function check(draft: Draft): Checked {
  const title = draft.title.trim();
  if (title.length > LIMITS.titleChars) return refuse("add_title_too_long", { max: LIMITS.titleChars });
  const tags = tagsOf(draft.tags);
  if (tags.length > LIMITS.tags) return refuse("add_too_many_tags", { max: LIMITS.tags });
  if (tags.some((tag) => tag.length > LIMITS.tagChars)) return refuse("add_tag_too_long", { max: LIMITS.tagChars });
  const common = { mode: draft.mode, ...(title === "" ? {} : { title }), ...(tags.length === 0 ? {} : { tags }) };

  switch (draft.mode) {
    case "note":
      if (title === "") return refuse("add_need_title");
      if (draft.body.trim() === "") return refuse("add_need_text");
      if (draft.body.length > LIMITS.noteChars) return refuse("add_text_too_long", { max: LIMITS.noteChars });
      return { ok: true, payload: { ...common, body: draft.body } };
    case "address": {
      const address = draft.address.trim();
      if (address === "") return refuse("add_need_address");
      if (address.length > LIMITS.addressChars || !isWebAddress(address)) return refuse("add_bad_address");
      return { ok: true, payload: { ...common, address } };
    }
    case "file":
      if (!draft.file) return refuse("add_need_file");
      return { ok: true, payload: { ...common, filename: draft.file.filename, content: draft.file.content } };
  }
}

// Reads a file into base64 text. Refuses what is too big before reading it.
export function readFile(file: File): Promise<FileDraft> {
  return new Promise((resolve, reject) => {
    if (file.size > LIMITS.fileBytes) return reject(new RangeError("too big"));
    if (file.size === 0) return reject(new Error("empty"));
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("unreadable"));
    reader.onload = () => {
      const result = String(reader.result);
      const content = result.slice(result.indexOf(",") + 1);
      if (content === "" || content.length > base64Length(LIMITS.fileBytes)) return reject(new Error("unreadable"));
      resolve({ filename: file.name, content, bytes: file.size });
    };
    reader.readAsDataURL(file);
  });
}

// The reply crossed a process boundary, so its shape is checked here too.
export function parseIngestResult(value: unknown): IngestResult | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (typeof value.count !== "number") return undefined;
  const num = (field: unknown) => (typeof field === "number" && Number.isFinite(field) ? field : 0);
  return {
    count: num(value.count),
    created: num(value.created),
    updated: num(value.updated),
    unchanged: num(value.unchanged),
    chunks: num(value.chunks),
    degraded: value.degraded === true,
    reason: text(value.reason),
  };
}

// What to tell the person: one document, or a count of what a feed file held.
export function outcomeOf(result: IngestResult): Problem {
  if (result.count === 0) return { id: "add_nothing" };
  if (result.count === 1) {
    if (result.created > 0) return { id: "add_created", params: { chunks: result.chunks } };
    if (result.updated > 0) return { id: "add_updated", params: { chunks: result.chunks } };
    return { id: "add_unchanged" };
  }
  return {
    id: "add_many",
    params: { count: result.count, created: result.created, updated: result.updated, unchanged: result.unchanged },
  };
}
