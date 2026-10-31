import { INGEST_ID, INGEST_KIND, INGEST_LIMITS as LIMITS, INGEST_MODES, type IngestMode } from "../../shared/ingest.js";
import { POST_REFUSALS, type PostMedia, type PostReply, type PostView } from "../../shared/posts.js";
import { isRecord, text } from "../../shared/records.js";
import type { IngestResult, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import type { MessageId } from "../i18n/i18n.js";
import type { LoadResult } from "./use_card.js";

// The ways of adding the strip offers: the three the form builds, and a post link, which has a
// pane of its own and is not part of the form's draft.
export const ADD_MODES = [...INGEST_MODES, "post"] as const;
export type AddMode = (typeof ADD_MODES)[number];

// A big file can take a while to read, split and index, and the orchestrator answers one request
// at a time. If the wait runs out the document may still have been added.
export const INGEST_TIMEOUT_MS = 120_000;

export async function addToBase(
  transport: Transport,
  payload: Record<string, unknown>,
): Promise<LoadResult<IngestResult | Off>> {
  const answer = await transport.request("knowledge.ingest", payload, { timeoutMs: INGEST_TIMEOUT_MS });
  if (!answer.ok) return answer;
  const result = parseIngestResult(answer.result);
  return result
    ? { ok: true, data: result }
    : { ok: false, error: { code: "unexpected", message: "unexpected ingest reply" } };
}

// A file the person chose, already read: it is read when chosen, not when sent, so the click that
// sends it is the click the shell sees.
export type FileDraft = { filename: string; content: string; bytes: number };

export type Draft = {
  mode: IngestMode;
  title: string;
  body: string;
  address: string;
  tags: string;
  // blank means the default of the mode, the address itself, and an id made from what is added
  kind: string;
  reference: string;
  id: string;
  file?: FileDraft;
};

// What a kind is when none is chosen, per way of adding.
export const DEFAULT_KINDS: Record<IngestMode, string> = { note: "note", address: "article", file: "document" };

// Kinds worth offering; any one lowercase word is allowed.
export const KIND_SUGGESTIONS = ["note", "article", "book", "paper", "document", "list"] as const;

// What the screen says when it cannot send: a message and the numbers it needs.
export type Problem = { id: MessageId; params?: Record<string, number> };

export type Checked = { ok: true; payload: Record<string, unknown> } | { ok: false; problem: Problem };

const refuse = (id: MessageId, params?: Record<string, number>): Checked => ({
  ok: false,
  problem: params ? { id, params } : { id },
});

function tagsOf(raw: string): string[] {
  const tags: string[] = [];
  for (const item of raw.split(",")) {
    const tag = item.trim();
    if (tag !== "" && !tags.includes(tag)) tags.push(tag);
  }
  return tags;
}

export function isWebAddress(raw: string): boolean {
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
  const kind = draft.kind.trim();
  if (kind !== "" && !INGEST_KIND.test(kind)) return refuse("add_bad_kind");
  const reference = draft.reference.trim();
  if (reference.length > LIMITS.referenceChars) return refuse("add_ref_too_long", { max: LIMITS.referenceChars });
  const id = draft.id.trim();
  if (id !== "" && !INGEST_ID.test(id)) return refuse("add_bad_id", { max: LIMITS.idChars });
  const common = {
    mode: draft.mode,
    ...(title === "" ? {} : { title }),
    ...(tags.length === 0 ? {} : { tags }),
    ...(kind === "" ? {} : { kind }),
    ...(reference === "" ? {} : { reference }),
    ...(id === "" ? {} : { id }),
  };

  switch (draft.mode) {
    case "note":
      if (title === "") return refuse("add_need_title");
      if (draft.body.trim() === "") return refuse("add_need_text");
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

// Reads a file into base64 text. Only the machine's memory bounds its size: a file too large for
// a string in memory fails here as a RangeError.
export function readFile(file: File): Promise<FileDraft> {
  return new Promise((resolve, reject) => {
    if (file.size === 0) return reject(new Error("empty"));
    const reader = new FileReader();
    reader.onerror = () => reject(new Error("unreadable"));
    reader.onload = () => {
      try {
        const result = String(reader.result);
        const content = result.slice(result.indexOf(",") + 1);
        if (content === "") return reject(new Error("unreadable"));
        resolve({ filename: file.name, content, bytes: file.size });
      } catch (error) {
        reject(error);
      }
    };
    try {
      reader.readAsDataURL(file);
    } catch (error) {
      reject(error);
    }
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
    ...(num(value.pendingEmbeddings) > 0 ? { pendingEmbeddings: num(value.pendingEmbeddings) } : {}),
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

// What the post field holds, checked before anything is sent. The shell checks the address again.
export type CheckedLink = { ok: true; url: string } | { ok: false; problem: MessageId };

export function checkPostLink(raw: string): CheckedLink {
  const url = raw.trim();
  if (url === "") return { ok: false, problem: "post_need_link" };
  if (url.length > LIMITS.addressChars || !isWebAddress(url)) return { ok: false, problem: "add_bad_address" };
  return { ok: true, url };
}

function parsePostView(value: unknown): PostView | undefined {
  if (!isRecord(value)) return undefined;
  const documentId = text(value.documentId);
  const url = text(value.url);
  if (documentId === "" || url === "") return undefined;
  const media: PostMedia[] = [];
  for (const item of Array.isArray(value.media) ? value.media : []) {
    if (isRecord(item) && typeof item.src === "string" && item.src !== "") {
      media.push({ kind: item.kind === "video" ? "video" : "image", src: item.src });
    }
  }
  const missing = typeof value.missing === "number" && Number.isFinite(value.missing) ? Math.max(0, Math.trunc(value.missing)) : 0;
  return {
    documentId,
    url,
    author: text(value.author),
    caption: typeof value.caption === "string" ? value.caption : "",
    published: text(value.published),
    media,
    missing,
  };
}

// The reply crossed a process boundary, so its shape is checked here too.
export function parsePostReply(value: unknown): PostReply | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (value.status === "refused") {
    const refused = POST_REFUSALS.find((reason) => reason === value.refused);
    return refused ? { status: "refused", refused } : undefined;
  }
  if (value.status !== "ok") return undefined;
  const post = parsePostView(value.post);
  return post ? { status: "ok", post } : undefined;
}

// What a post is called on screen: who wrote it, or else where it is.
export const postTitle = (post: Pick<PostView, "author" | "url">): string => post.author || post.url;

// A saved post says so only when something was written; the same text again is already there.
export function postOutcome(result: IngestResult): "post_saved" | "add_unchanged" {
  return result.created > 0 || result.updated > 0 ? "post_saved" : "add_unchanged";
}
