import type { ActionType } from "../shared/actions.js";
import { isRecord, text } from "../shared/records.js";
import { buildIngest } from "./ingest.js";
import type {
  DigestPage,
  DigestStatus,
  IngestResult,
  KnowledgeSearch,
  KnowledgeStatus,
  MemePage,
  MemeStatus,
  MemoryScore,
  MemoryStatus,
  Off,
  ServiceState,
  StatusReply,
} from "../shared/replies.js";

export type ActionDefinition = {
  // The most the payload may weigh, in characters, before the builder sees it. Small unless the action
  // takes a file.
  maxPayloadChars?: number;
  // Builds the payload from what the screen asked for. The screen cannot add fields of its own:
  // anything it sends beyond these is dropped. Returns undefined when the request is not valid.
  payload(raw: Record<string, unknown>): Record<string, unknown> | undefined;
  // Reduces the orchestrator's reply to what the card reads.
  project(result: unknown): unknown;
  // Addresses of pictures and videos in the projected reply that the screen may later ask the shell to preview.
  media?(projected: unknown): string[];
};

const MAX_DETAIL_CHARS = 200;
const MAX_QUERY_CHARS = 500;

// The states the feeds service reports for a source; anything else is shown as unknown.
const SOURCE_STATES = new Set(["ok", "failed", "pending", "disabled", "invalid"]);
const MAX_SOURCES = 50;

// The action the TUI scores the experience memory by: the hashtag suggestion.
const SCORED_ACTION = "hashtag.suggest";

function shorten(value: string, limit = MAX_DETAIL_CHARS): string {
  return value.length > limit ? `${value.slice(0, limit - 1)}…` : value;
}

function count(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function clampInt(value: unknown, min: number, max: number, fallback: number): number {
  const whole = typeof value === "number" && Number.isFinite(value) ? Math.trunc(value) : fallback;
  return Math.min(Math.max(whole, min), max);
}

function records<T>(value: unknown, map: (item: Record<string, unknown>) => T): T[] {
  return Array.isArray(value) ? value.filter(isRecord).map(map) : [];
}

function words(value: unknown): string[] {
  return Array.isArray(value) ? value.map(text).filter((word) => word !== "") : [];
}

// A meme's tags come as one string; a list is joined the same way.
function tagsText(value: unknown): string {
  return Array.isArray(value) ? words(value).join(", ") : text(value);
}

// The orchestrator answers with the result of the one domain that handled the event. A verbose
// reply, or an event that several domains handled, nests it under the domain name instead.
function domainResult(result: unknown, domain: string): Record<string, unknown> | undefined {
  if (!isRecord(result)) return undefined;
  const inner = result.result;
  if (!isRecord(inner)) return undefined;
  if (result.domain === domain) return inner;
  const nested = inner[domain];
  return isRecord(nested) ? nested : undefined;
}

// Reads one domain's reply. A switched-off service comes back as { skipped, reason }; that is an
// answer, not a failure, so it is passed on as such.
function inDomain<T>(
  result: unknown,
  domain: string,
  shape: (body: Record<string, unknown>) => T,
): T | Off {
  const body = domainResult(result, domain);
  if (!body) throw new Error(`unexpected ${domain} reply`);
  if (body.skipped === true) return { off: true, reason: shorten(text(body.reason)) };
  return shape(body);
}

const noPayload = (): Record<string, unknown> => ({});

function systemStatus(body: Record<string, unknown>): StatusReply {
  const services: Record<string, ServiceState> = {};
  if (isRecord(body.services)) {
    for (const [name, entry] of Object.entries(body.services)) {
      if (!isRecord(entry)) continue;
      services[name] = {
        status: text(entry.status),
        detail: shorten(text(entry.error) || text(entry.detail)),
        ...(typeof entry.enabled === "boolean" ? { enabled: entry.enabled } : {}),
      };
    }
  }
  return { status: text(body.status) || "ok", services };
}

function memoryStatus(body: Record<string, unknown>): MemoryStatus {
  return {
    experiences: count(body.experiences),
    pending: count(body.pending),
    resolved: count(body.resolved),
    expired: count(body.expired),
    embedded: count(body.embedded),
    needsReindex: count(body.needs_reindex),
    embedModel: text(body.embed_model),
    embeddings: body.embeddings === true,
    embedderReachable: body.embedder_reachable === true,
  };
}

function memoryScore(body: Record<string, unknown>): MemoryScore {
  return {
    scored: count(body.scored),
    unscored: count(body.unscored),
    minScored: count(body.min_scored),
    brierPrediction: count(body.brier_prediction),
    brierBaseline: count(body.brier_baseline),
    skill: count(body.skill),
    beatsBaseline: body.beats_baseline === true,
    enoughData: body.enough_data === true,
  };
}

function knowledgeStatus(body: Record<string, unknown>): KnowledgeStatus {
  return {
    documents: count(body.documents),
    chunks: count(body.chunks),
    embedded: count(body.embedded),
    pendingEmbeddings: count(body.pending_embeddings),
    embedModel: text(body.embed_model),
    embedderReachable: typeof body.embedder_reachable === "boolean" ? body.embedder_reachable : null,
  };
}

function knowledgeSearch(body: Record<string, unknown>): KnowledgeSearch {
  return {
    query: shorten(text(body.query), MAX_QUERY_CHARS),
    hits: records(body.hits, (hit) => ({
      rank: count(hit.rank),
      title: shorten(text(hit.title), 160),
      heading: shorten(text(hit.heading), 160),
      kind: text(hit.kind),
      sourceRef: shorten(text(hit.source_ref), 120),
      tags: words(hit.tags).slice(0, 8),
      text: shorten(text(hit.text), 400),
      score: typeof hit.score === "number" && Number.isFinite(hit.score) ? hit.score : null,
      relevant: hit.relevant === true,
    })),
    relevantCount: count(body.relevant_count),
    threshold: count(body.threshold),
    degraded: body.degraded === true,
    reason: shorten(text(body.reason)),
  };
}

function memeStatus(body: Record<string, unknown>): MemeStatus {
  return {
    status: text(body.status) || "ok",
    unsent: count(body.unsent_count),
    sent: count(body.sent_count),
  };
}

function memePage(body: Record<string, unknown>): MemePage {
  return {
    total: count(body.total),
    offset: count(body.offset),
    memes: records(body.memes, (meme) => ({
      url: text(meme.url),
      title: shorten(text(meme.title), 160),
      tags: shorten(tagsText(meme.tags), 120),
      source: shorten(text(meme.source), 60),
      createdAt: text(meme.date_created),
      sentAt: text(meme.date_sent),
    })),
  };
}

function digestStatus(body: Record<string, unknown>): DigestStatus {
  return {
    configured: body.configured === true,
    directoryError: shorten(text(body.directory_error)),
    ok: count(body.ok),
    failed: count(body.failed),
    pending: count(body.pending),
    disabled: count(body.disabled),
    invalid: count(body.invalid),
    items: count(body.items),
    sources: records(body.plugins, (plugin) => {
      const state = text(plugin.state);
      return {
        id: shorten(text(plugin.id), 64),
        name: shorten(text(plugin.name) || text(plugin.id), 80),
        state: SOURCE_STATES.has(state) ? state : "unknown",
        running: plugin.running === true,
        items: count(plugin.items),
        skipped: count(plugin.skipped),
        lastOk: text(plugin.last_ok),
        error: shorten(text(plugin.last_error)),
      };
    }).slice(0, MAX_SOURCES),
  };
}

function digestPage(body: Record<string, unknown>): DigestPage {
  return {
    items: records(body.items, (item) => ({
      id: shorten(text(item.id), 120),
      title: shorten(text(item.title), 300),
      summary: shorten(text(item.summary), 600),
      source: shorten(text(item.source), 80),
      published: text(item.published),
    })),
    total: count(body.total),
    offset: count(body.offset),
    more: count(body.more),
    updated: text(body.updated),
  };
}

// The knowledge service answers one document with its own fields and many (a feed file) with a
// summary and the list. Either way the screen gets the same counts.
function ingestResult(body: Record<string, unknown>): IngestResult {
  const documents = records(body.documents, (document) => ({
    created: document.created === true,
    changed: document.changed === true,
  }));
  const many = Array.isArray(body.documents);
  const list = many ? documents : [{ created: body.created === true, changed: body.changed === true }];
  return {
    count: list.length,
    created: list.filter((document) => document.created).length,
    updated: list.filter((document) => document.changed && !document.created).length,
    unchanged: list.filter((document) => !document.changed).length,
    chunks: count(body.chunks),
    degraded: body.degraded === true,
    reason: shorten(text(body.reason)),
  };
}

export const ACTIONS: Record<ActionType, ActionDefinition> = {
  "ningo.status": {
    payload: noPayload,
    project: (result) => inDomain(result, "system", systemStatus),
  },
  "memory.status": {
    payload: noPayload,
    project: (result) => inDomain(result, "memory", memoryStatus),
  },
  "memory.stats": {
    payload: () => ({ action: SCORED_ACTION }),
    project: (result) => inDomain(result, "memory", memoryScore),
  },
  "knowledge.status": {
    payload: noPayload,
    project: (result) => inDomain(result, "knowledge", knowledgeStatus),
  },
  "knowledge.search": {
    payload: (raw) => {
      const query = text(raw.query);
      if (query === "" || query.length > MAX_QUERY_CHARS) return undefined;
      return { query, k: clampInt(raw.k, 1, 10, 5) };
    },
    project: (result) => inDomain(result, "knowledge", knowledgeSearch),
  },
  "meme.status": {
    payload: noPayload,
    project: (result) => inDomain(result, "meme", memeStatus),
  },
  "meme.list": {
    payload: (raw) => ({
      scope: raw.scope === "sent" ? "sent" : "unsent",
      limit: clampInt(raw.limit, 1, 24, 12),
      offset: clampInt(raw.offset, 0, 100_000, 0),
    }),
    project: (result) => inDomain(result, "meme", memePage),
    media: (projected) =>
      isRecord(projected) && Array.isArray(projected.memes)
        ? projected.memes.filter(isRecord).map((meme) => text(meme.url)).filter((url) => url !== "")
        : [],
  },
  "digest.status": {
    payload: noPayload,
    project: (result) => inDomain(result, "digest", digestStatus),
  },
  "digest.items": {
    // The first page is short on purpose; the screen asks for a longer one to see more.
    payload: (raw) => ({
      limit: clampInt(raw.limit, 1, 50, 8),
      offset: clampInt(raw.offset, 0, 1000, 0),
    }),
    project: (result) => inDomain(result, "digest", digestPage),
  },
  "knowledge.ingest": {
    // A file travels as base64 text: the cap is the largest file, a third more.
    maxPayloadChars: 12_000_000,
    payload: buildIngest,
    project: (result) => inDomain(result, "knowledge", ingestResult),
  },
};
