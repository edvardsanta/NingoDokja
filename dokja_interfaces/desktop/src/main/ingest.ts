import { createHash } from "node:crypto";

import { INGEST_ID, INGEST_KIND, INGEST_LIMITS as LIMITS, INGEST_MODES, base64Length } from "../shared/ingest.js";
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

// An optional text field: empty when it is absent or blank, undefined when it is not text at all.
function optional(value: unknown): string | undefined {
  if (value === undefined || value === null) return "";
  return typeof value === "string" ? value.trim() : undefined;
}

// Builds the payload of knowledge.ingest from what the screen asked for: a note, an address or a
// file, and optionally its kind, where it came from (reference) and the id it is known by. The
// screen chooses these, within the service's own rules; what it sends beyond these fields (the
// service's source, source_id, source_ref) is dropped. Undefined when the request is not valid.
//
// Without an id of its own, a note or a file gets one made from what it holds, so adding the same
// thing twice changes nothing and two notes that share a title do not replace each other. An
// address keeps the service's own id, made of the address, so adding it again refreshes that page.
// An id the person chose replaces the document that has it: that is what an id is for.
export function buildIngest(raw: Record<string, unknown>): Record<string, unknown> | undefined {
  const mode = raw.mode;
  if (!(INGEST_MODES as readonly unknown[]).includes(mode)) return undefined;
  const tags = cleanTags(raw.tags);
  const title = text(raw.title);
  const chosenKind = optional(raw.kind);
  const reference = optional(raw.reference);
  const chosenId = optional(raw.id);
  if (!tags || title.length > LIMITS.titleChars) return undefined;
  if (chosenKind === undefined || (chosenKind !== "" && !INGEST_KIND.test(chosenKind))) return undefined;
  if (reference === undefined || reference.length > LIMITS.referenceChars) return undefined;
  if (chosenId === undefined || (chosenId !== "" && !INGEST_ID.test(chosenId))) return undefined;
  const common = {
    ...(title === "" ? {} : { title }),
    ...(tags.length === 0 ? {} : { tags }),
    ...(reference === "" ? {} : { source_ref: reference }),
  };
  const idOr = (derived?: string) => {
    const id = chosenId !== "" ? chosenId : derived;
    return id === undefined ? {} : { source_id: id };
  };

  switch (mode) {
    case "note": {
      const body = typeof raw.body === "string" ? raw.body : "";
      if (title === "" || body.trim() === "" || body.length > LIMITS.noteChars) return undefined;
      return {
        ...common,
        body,
        kind: chosenKind || "note",
        ...idOr(`note:${slug(title) || "note"}-${fingerprint(title, body)}`),
      };
    }
    case "address": {
      const source = webAddress(raw.address);
      return source === undefined ? undefined : { ...common, source, kind: chosenKind || "article", ...idOr() };
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
        kind: chosenKind || "document",
        ...idOr(`file:${slug(stem) || "file"}-${fingerprint(filename, content)}`),
      };
    }
  }
}
