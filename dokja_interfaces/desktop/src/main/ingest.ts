import { createHash } from "node:crypto";

import { INGEST_LIMITS as LIMITS, INGEST_MODES, base64Length } from "../shared/ingest.js";
import { text } from "../shared/records.js";

const BASE64 = /^[A-Za-z0-9+/]+={0,2}$/;

// Lower case letters and digits joined by dashes, accents folded away: the readable part of an id.
function slug(value: string): string {
  return value
    .normalize("NFKD")
    .replace(/\p{M}/gu, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60)
    .replace(/-+$/, "");
}

// Ten hex digits that follow what was sent, so the same thing sent twice has the same id.
function fingerprint(...parts: string[]): string {
  return createHash("sha256").update(parts.join("\n")).digest("hex").slice(0, 10);
}

// Up to eight tags of at most forty characters, from a list or from text split at commas.
// Undefined when there are too many or one is too long, so the screen hears about it.
export function cleanTags(raw: unknown): string[] | undefined {
  if (raw === undefined || raw === null) return [];
  const items = typeof raw === "string" ? raw.split(",") : Array.isArray(raw) ? raw : undefined;
  if (!items) return undefined;
  const tags: string[] = [];
  for (const item of items) {
    const tag = text(item);
    if (tag === "") continue;
    if (tag.length > LIMITS.tagChars || tag.includes(",")) return undefined;
    if (!tags.includes(tag)) tags.push(tag);
  }
  return tags.length <= LIMITS.tags ? tags : undefined;
}

// A file name and nothing of the path in front of it, short enough, with its extension kept.
export function baseName(raw: unknown): string | undefined {
  const name = text(raw).split(/[\\/]/).pop()?.trim() ?? "";
  if (name === "" || name === "." || name === "..") return undefined;
  if (name.length <= LIMITS.filenameChars) return name;
  const dot = name.lastIndexOf(".");
  const extension = dot > 0 && name.length - dot <= 12 ? name.slice(dot) : "";
  return name.slice(0, LIMITS.filenameChars - extension.length) + extension;
}

function webAddress(raw: unknown): string | undefined {
  const address = text(raw);
  if (address === "" || address.length > LIMITS.addressChars) return undefined;
  let url: URL;
  try {
    url = new URL(address);
  } catch {
    return undefined;
  }
  // Only the web. Any other scheme would be handed to one of the owner's own plugins.
  if (url.protocol !== "http:" && url.protocol !== "https:") return undefined;
  if (url.hostname === "" || url.username !== "" || url.password !== "") return undefined;
  return url.href;
}

// Builds the payload of knowledge.ingest from what the screen asked for: a note, an address or a
// file. The screen cannot choose the kind, the id or the reference; what it sends beyond these
// fields is dropped. Undefined when the request is not valid.
//
// An id comes from what was added, so adding the same thing twice changes nothing and two notes
// that share a title do not replace each other. An address keeps the service's own id, made of
// the address, so adding it again refreshes that page.
export function buildIngest(raw: Record<string, unknown>): Record<string, unknown> | undefined {
  const mode = raw.mode;
  if (!(INGEST_MODES as readonly unknown[]).includes(mode)) return undefined;
  const tags = cleanTags(raw.tags);
  const title = text(raw.title);
  if (!tags || title.length > LIMITS.titleChars) return undefined;
  const common = { ...(title === "" ? {} : { title }), ...(tags.length === 0 ? {} : { tags }) };

  switch (mode) {
    case "note": {
      const body = typeof raw.body === "string" ? raw.body : "";
      if (title === "" || body.trim() === "" || body.length > LIMITS.noteChars) return undefined;
      return { ...common, body, kind: "note", source_id: `note:${slug(title) || "note"}-${fingerprint(title, body)}` };
    }
    case "address": {
      const source = webAddress(raw.address);
      return source === undefined ? undefined : { ...common, source, kind: "article" };
    }
    default: {
      const filename = baseName(raw.filename);
      const content = typeof raw.content === "string" ? raw.content : "";
      if (!filename || content === "" || content.length > base64Length(LIMITS.fileBytes) || !BASE64.test(content)) {
        return undefined;
      }
      const stem = filename.replace(/\.[^.]*$/, "");
      return {
        ...common,
        content_b64: content,
        filename,
        kind: "document",
        source_id: `file:${slug(stem) || "file"}-${fingerprint(filename, content)}`,
      };
    }
  }
}
