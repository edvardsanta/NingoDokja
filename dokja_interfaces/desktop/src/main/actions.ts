import { createHash } from "node:crypto";
import { runtimeModel, runtimeModels, runtimeChat, runtimeChatConfigure, runtimeProviderConfigure, runtimeProviderSaved, runtimeProviders, runtimeSet, runtimeSettings, runtimeSettingsSet, runtimeSnapshot, runtimeModule, runtimeLogs } from "./runtime_actions.js";
import type { ActionType } from "../shared/actions.js";
import { isRecord, text } from "../shared/records.js";
import { memoryListPayload, memoryGetPayload, memoryPage, memoryDetail, memoryResolvePayload, memoryResolution } from "./memory_experiences.js";
import { buildIngest } from "./ingest.js";
import { linkImage } from "./link_image.js";
import { INGEST_ID, INGEST_LIMITS } from "../shared/ingest.js";
import type {
  DeleteResult,
  DigestPage,
  DigestStatus,
  IngestResult,
  InterfaceLink,
  JobState,
  JobSwitched,
  KnowledgeDocuments,
  KnowledgeClassification,
  ClassificationChoice,
  KnowledgeSearch,
  KnowledgeStatus,
  MemePage,
  MemeHashtagTag,
  MemePoolRefresh,
  MemeStatus,
  MemoryScore,
  MemoryStatus,
  Off,
  ReindexResult,
  ServiceState,
  ServiceSwitched,
  StatusReply,
} from "../shared/replies.js";

export type ActionDefinition = {
  // The most the payload may weigh, in characters, before the builder sees it. Small unless the action
  // takes a file.
  maxPayloadChars?: number;
  // Some actions read or write depending on their validated payload.
  writes?(payload: Record<string, unknown>): boolean;
  // Builds the payload from what the screen asked for. The screen cannot add fields of its own:
  // anything it sends beyond these is dropped. Returns undefined when the request is not valid.
  payload(raw: Record<string, unknown>): Record<string, unknown> | undefined;
  // Reduces the orchestrator's reply to what the card reads.
  project(result: unknown): unknown;
  // Addresses of pictures and videos in the projected reply that the screen may later ask the shell to preview.
  media?(projected: unknown): string[];
};

const MAX_DETAIL_CHARS = 200;

// The orchestrator checks the name against the services and jobs it knows; this only keeps the
// wrong shape from reaching it. An interval is "default" (drop the override) or whole minutes or
// hours: the empty text must never get through, because the orchestrator reads it as "default".
// Five digits cover the longest interval it keeps (thirty days is 43200 minutes); it checks the range.
const SWITCH_NAME = /^[a-z][a-z_.]{0,39}$/;
const JOB_INTERVAL = /^(default|[0-9]{1,5}[mh])$/;
const MAX_JOBS = 20;
// The state an interface reports comes from a closed list of short names; anything else is dropped.
const INTERFACE_STATE = /^[a-z][a-z_]{0,31}$/;
// The only interface the desktop links. The name is fixed here and never taken from the screen.
const LINKABLE_INTERFACE = "whatsapp";
const MAX_QUERY_CHARS = 500;
// The most of one document's text a tab shows: about a hundred pages.
const MAX_DOCUMENT_CHARS = 200_000;

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

function chatPayload(raw: Record<string, unknown>): Record<string, unknown> | undefined {
  const content = text(raw.content).trim();
  const conversationId = text(raw.conversation_id);
  const readingId = text(raw.reading_id);
  if (!content || content.length > 12000 || conversationId.length > 512 || readingId.length > 512) return undefined;
  return { content, conversation_id: conversationId, ...(readingId ? { reading_id: readingId } : {}) };
}

function chatReply(result: unknown): { reply: string; sessionNotice: string } | Off {
  return inDomain(result, "chat", (body) => {
    const session = isRecord(body.session) ? body.session : {};
    return { reply: shorten(text(body.reply), 12000), sessionNotice: session.created === true ? "new" : session.revoked === true ? "reset" : "" };
  });
}

// The three link actions share one payload (the interface, fixed) and one projection (the state and
// the picture of the code), so a screen cannot ask the orchestrator for anything else.
const linkPayload = (raw: Record<string, unknown>): Record<string, unknown> | undefined =>
  raw.interface === LINKABLE_INTERFACE ? { interface: LINKABLE_INTERFACE } : undefined;

function linkProjection(result: unknown): InterfaceLink | Off {
  return inDomain(result, "system", (body): InterfaceLink => {
    const state = text(body.state);
    const image = linkImage(body.matrix);
    return { state: INTERFACE_STATE.test(state) ? state : "unknown", ...(image ? { image } : {}) };
  });
}

function systemStatus(body: Record<string, unknown>): StatusReply {
  const services: Record<string, ServiceState> = {};
  if (isRecord(body.services)) {
    for (const [name, entry] of Object.entries(body.services)) {
      if (!isRecord(entry)) continue;
      services[name] = {
        status: text(entry.status),
        detail: shorten(text(entry.error) || text(entry.detail)),
        ...(typeof entry.enabled === "boolean" ? { enabled: entry.enabled } : {}),
        ...(entry.kind === "interface" ? { kind: "interface" as const } : {}),
        ...(entry.kind === "interface" && typeof entry.configured === "boolean" ? { configured:entry.configured } : {}),
        ...(entry.kind === "interface" && INTERFACE_STATE.test(text(entry.state)) ? { state: text(entry.state) } : {}),
      };
    }
  }
  const jobs = Array.isArray(body.jobs) ? records(body.jobs, jobState).slice(0, MAX_JOBS) : undefined;
  return { status: text(body.status) || "ok", services, ...(jobs ? { jobs } : {}) };
}

function jobState(job: Record<string, unknown>): JobState {
  return {
    name: shorten(text(job.name), 40),
    enabled: job.enabled === true,
    interval: shorten(text(job.interval), 30),
    intervalOverride: job.interval_override === true,
    nextAt: shorten(text(job.next_at), 40),
    lastAt: shorten(text(job.last_at), 40),
    lastOutcome: shorten(text(job.last_outcome), 30),
    lastError: shorten(text(job.last_error)),
  };
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
      // The document the passage came from, so the hit can open it; empty when it is no id.
      sourceId: INGEST_ID.test(text(hit.source_id)) ? text(hit.source_id) : "",
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
    ...(Array.isArray(body.providers) ? {providers: records(body.providers, (plugin) => ({
      id: shorten(text(plugin.id),64), name: shorten(text(plugin.name) || text(plugin.id),80),
      state: SOURCE_STATES.has(text(plugin.state)) ? text(plugin.state) : "unknown",
      running: plugin.running === true, items: 0, skipped: 0,
      lastOk: text(plugin.last_ok), error: shorten(text(plugin.last_error)),
    })).slice(0,MAX_SOURCES)} : {}),
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

// A web address the Library can read, or nothing: only http and https, and not absurdly long.
function webAddress(raw: string): string {
  if (raw.length > 2000) return "";
  try {
    const parsed = new URL(raw);
    return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.hostname && !parsed.username && !parsed.password ? raw : "";
  } catch {
    return "";
  }
}

// The shell keeps source text and addresses by reading id; the screen only names an item.
const READINGS = new Map<string, Record<string, unknown>>();
const MAX_READINGS = 500;

function rememberReading(item: Record<string, unknown>): void {
  const id = shorten(text(item.id), 120);
  if (id === "") return;
  READINGS.delete(id);
  const address = webAddress(text(item.url));
  const body = typeof item.body === "string" ? item.body.trim() : "";
  const payload = body && body.length <= INGEST_LIMITS.noteChars
    ? buildIngest({
      mode: "note", title: shorten(text(item.title), 300), body,
      kind: "article", reference: address,
    })
    : address ? buildIngest({ mode: "address", address }) : undefined;
  if (payload) {
    // Identity follows the source and reading, so refreshing its text updates the same document.
    if (body && payload.body) {
      payload.source_id = `reading:${createHash("sha256").update(JSON.stringify([text(item.plugin), id])).digest("hex")}`;
    }
    READINGS.set(id, payload);
  }
  while (READINGS.size > MAX_READINGS) {
    const oldest = READINGS.keys().next().value;
    if (oldest === undefined) break;
    READINGS.delete(oldest);
  }
}

function readingIngest(raw: Record<string, unknown>): Record<string, unknown> | undefined {
  const payload = READINGS.get(text(raw.reading));
  return payload ? { ...payload } : undefined;
}

// The questions a source suggests for its item: at most three short lines, or nothing.
function readingPrompts(value: unknown): { prompts?: string[] } {
  if (!Array.isArray(value)) return {};
  const prompts = value.map((each) => shorten(text(each), 120)).filter((each) => each !== "").slice(0, 3);
  return prompts.length > 0 ? { prompts } : {};
}

function digestPage(body: Record<string, unknown>): DigestPage {
  return {
    items: records(body.items, (item) => {
      rememberReading(item);
      return {
      id: shorten(text(item.id), 120),
      title: shorten(text(item.title), 300),
      summary: shorten(text(item.summary), 600),
      source: shorten(text(item.source), 80),
      published: text(item.published),
      ...(typeof item.body === "string" && item.body.trim() ? { previewText: item.body.trim().slice(0, 100_000) } : {}),
      ...(webAddress(text(item.media_url)) ? { mediaUrl: webAddress(text(item.media_url)) } : {}),
      ...readingPrompts(item.prompts),
      };
    }),
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

// The text the service rebuilt from a document's passages, when it was asked for, cut at a size the
// screen can show; `truncated` says it was cut.
function documentText(value: unknown): { text?: string; truncated?: boolean } {
  if (typeof value !== "string") return {};
  return value.length > MAX_DOCUMENT_CHARS
    ? { text: value.slice(0, MAX_DOCUMENT_CHARS), truncated: true }
    : { text: value, truncated: false };
}

function knowledgeDocuments(body: Record<string, unknown>): KnowledgeDocuments {
  return {
    documents: records(body.documents, (document) => ({
      id: shorten(text(document.source_id), 200),
      title: shorten(text(document.title), 300),
      kind: shorten(text(document.kind), 32),
      reference: shorten(text(document.source_ref), 300),
      tags: words(document.tags).slice(0, 20).map((tag) => shorten(tag, 40)),
      chunks: count(document.chunks),
      embedded: count(document.embedded),
      updatedAt: text(document.updated_at),
      ...documentText(document.text),
    })),
    total: count(body.total),
    offset: count(body.offset),
  };
}

// Keep the taxonomy in the domain. The shell bounds labels without choosing them.
function classificationPayload(raw: Record<string, unknown>): Record<string, unknown> | undefined {
  const source = text(raw.source_id);
  if (!INGEST_ID.test(source)) return undefined;
  if (!("answers" in raw)) return "revision" in raw ? undefined : { source_id: source };
  if (!isRecord(raw.answers) || !/^[a-f0-9]{64}$/.test(text(raw.revision))) return undefined;
  const answers: Record<string, string> = {};
  for (const [field, value] of Object.entries(raw.answers)) {
    if (!["form", "content_kind"].includes(field) || typeof value !== "string" || !/^[a-z_]{1,40}$/.test(value)) return undefined;
    answers[field] = value;
  }
  return Object.keys(answers).length ? { source_id: source, answers, revision: raw.revision } : undefined;
}

function classificationResult(body: Record<string, unknown>): KnowledgeClassification {
  if (!INGEST_ID.test(text(body.source_id)) || !/^[a-f0-9]{64}$/.test(text(body.revision)) || !isRecord(body.classification)) {
    throw new Error("invalid classification snapshot");
  }
  const metadata = isRecord(body.metadata) ? body.metadata : {};
  const choice = (axis: string, field: string): ClassificationChoice => {
    const info = isRecord(metadata[field]) ? metadata[field] : {};
    return {
      value: typeof metadata[axis] === "string" ? shorten(metadata[axis], 40) : null,
      origin: shorten(text(info.origin), 40),
      classifierId: shorten(text(info.classifier_id), 100),
      classifierVersion: shorten(text(info.classifier_version), 100),
      reason: shorten(text(info.fallback_reason), 100),
    };
  };
  const questions: KnowledgeClassification["questions"] = [];
  if (!Array.isArray(body.classification.questions)) throw new Error("missing classification questions");
  for (const item of body.classification.questions) {
    if (!isRecord(item) || (item.field !== "form" && item.field !== "content_kind") || questions.some((q) => q.field === item.field)) throw new Error("invalid classification question");
    const values = words(item.allowed_values);
    if (!values.length || values.length > 32 || values.some((v) => !/^[a-z_]{1,40}$/.test(v))) throw new Error("invalid classification labels");
    questions.push({ field: item.field, question: shorten(text(item.question), 300), allowedValues: values });
  }
  if (body.classification.needs_user_input !== (questions.length > 0)) throw new Error("inconsistent classification state");
  return {
    sourceId: text(body.source_id), revision: text(body.revision),
    form: choice("form", "form_choice"), contentKind: choice("content_kind", "kind_choice"),
    needsUserInput: questions.length > 0, questions,
  };
}

function reindexResult(body: Record<string, unknown>): ReindexResult {
  return {
    embedded: count(body.embedded),
    remaining: count(body.remaining),
    degraded: body.degraded === true,
    reason: shorten(text(body.reason)),
  };
}

// The kinds of documents Ningo writes for his own use, not what the owner brought in: a meme's
// hashtag is one.
const INTERNAL_KINDS = ["meme_hashtag"] as const;

export const ACTIONS: Record<ActionType, ActionDefinition> = {
  "communications.channels": {
    payload: noPayload,
    project: (result) => inDomain(result, "communications", (body) => ({
      channels: records(body.channels, (channel) => ({
        id: text(channel.id),
        name: shorten(text(channel.name), 100),
        // the server a Discord channel belongs to, so the list can be grouped by it
        ...(/^[0-9]{1,20}$/.test(text(channel.guild_id)) ? { guildId: text(channel.guild_id), guildName: shorten(text(channel.guild_name), 100) } : {}),
        ...(count(channel.retention_days) > 0 ? { retentionDays: clampInt(channel.retention_days, 1, 365, 1) } : {}),
      })).slice(0, 300),
      // how many configured servers could not be read: a number, never their ids
      ...(Array.isArray(body.unavailable_guilds) && body.unavailable_guilds.length > 0 ? { unavailableServers: Math.min(body.unavailable_guilds.length, 20) } : {}),
    })),
  },
  "communications.history": {
    payload: (raw) => {
      // A Discord channel is a number; a WhatsApp conversation is the alias the owner gave it.
      if (!/^([0-9]{1,20}|whatsapp:[a-z0-9][a-z0-9_-]{0,31})$/.test(text(raw.channel_id))) return undefined;
      const before = text(raw.before);
      if (before && !/^[0-9]{1,20}$/.test(before)) return undefined;
      return { channel_id: raw.channel_id, before };
    },
    project: (result) => inDomain(result, "communications", (body) => ({
      channelId: text(body.channel_id), before: text(body.before),
      messages: records(body.messages, (message) => ({
        id: text(message.id), author: shorten(text(message.author), 100), bot: message.bot === true,
        content: shorten(text(message.content), 4000), timestamp: shorten(text(message.timestamp), 40),
        edited: message.edited === true, replyTo: text(message.reply_to),
        attachments: words(message.attachments).slice(0, 10).map((name) => shorten(name, 200)),
      })).slice(0, 30),
    })),
  },
  "communications.send": {
    payload: (raw) => {
      const content = text(raw.content).trim();
      const attachment = text(raw.attachment_url);
      const force = raw.force_nsfw === true;
      const skipHashtag = raw.skip_hashtag === true;
      const channel = text(raw.channel_id);
      // A WhatsApp conversation takes text only: it goes out as the owner's own account.
      if (/^whatsapp:[a-z0-9][a-z0-9_-]{0,31}$/.test(channel)) {
        if (!content || content.length > 2000 || attachment) return undefined;
        return { channel_ids: [channel], content };
      }
      if (!/^[0-9]{1,20}$/.test(channel) || content.length > 2000 || (!content && !attachment)) return undefined;
      if (attachment && (!/^https?:\/\//.test(attachment) || attachment.length > 2048)) return undefined;
      return { channel_ids: [raw.channel_id], content, ...(attachment ? { attachment_url: attachment } : {}), ...(force ? { force_nsfw: true } : {}), ...(skipHashtag ? { skip_hashtag: true } : {}) };
    },
    project: (result) => inDomain(result, "system", (body) => ({
      sent: words(body.sent_to).length === 1, skipped: words(body.skipped_unsafe).length > 0,
    })),
  },
  "meme.hashtag.tag": {
    payload: (raw) => {
      const url = text(raw.url).trim();
      const hashtag = text(raw.hashtag).trim();
      // The meme's text, written by the owner when it cannot be read from the image.
      const written = text(raw.text).trim();
      if (!/^https?:\/\//.test(url) || url.length > 2048 || hashtag.length < 1 || hashtag.length > 80 || written.length > 2000) return undefined;
      return { url, hashtag, ...(written ? { text: written } : {}) };
    },
    project: (result) => inDomain(result, "meme", (body): MemeHashtagTag => ({
      sourceUrl: text(body.source_url),
      hashtag: text(body.hashtag),
      embedded: body.embedded === true,
    })),
  },
  "communications.link.status": { payload: linkPayload, project: linkProjection },
  "communications.link.start": { payload: linkPayload, project: linkProjection },
  "communications.link.reset": { payload: linkPayload, project: linkProjection },
  "services.set": {
    payload: (raw) => {
      const name = text(raw.name);
      if (!SWITCH_NAME.test(name) || typeof raw.enabled !== "boolean") return undefined;
      return { name, enabled: raw.enabled };
    },
    project: (result) => inDomain(result, "system", (body): ServiceSwitched => ({
      name: shorten(text(body.name), 40),
      enabled: body.enabled === true,
    })),
  },
  "scheduler.jobs.set": {
    // Only what the screen supplied is sent: the orchestrator treats an interval that is present as
    // a change, so a switch must not carry one, and an interval must not carry a switch.
    payload: (raw) => {
      const name = text(raw.name);
      if (!SWITCH_NAME.test(name)) return undefined;
      const wire: Record<string, unknown> = { name };
      if (raw.enabled !== undefined) {
        if (typeof raw.enabled !== "boolean") return undefined;
        wire.enabled = raw.enabled;
      }
      if (raw.interval !== undefined) {
        if (typeof raw.interval !== "string" || !JOB_INTERVAL.test(raw.interval)) return undefined;
        wire.interval = raw.interval;
      }
      return Object.keys(wire).length > 1 ? wire : undefined;
    },
    project: (result) => inDomain(result, "system", (body): JobSwitched => ({
      name: shorten(text(body.name), 40),
      enabled: body.enabled === true,
      interval: shorten(text(body.interval), 30),
      intervalOverride: body.interval_override === true,
    })),
  },
  "ningo.status": {
    payload: noPayload,
    project: (result) => inDomain(result, "system", systemStatus),
  },
  "runtime.modules": { payload: noPayload, project: (result) => inDomain(result,"runtime",runtimeSnapshot) },
  "runtime.logs": { payload: runtimeModule, project: (result) => inDomain(result,"runtime",runtimeLogs) },
  "message.created": { payload: chatPayload, project: chatReply },
  "runtime.set": { payload: runtimeSet, project: (result) => inDomain(result,"runtime",runtimeSnapshot) },
  "runtime.providers.configure": { payload: runtimeProviderConfigure, project: (result) => inDomain(result,"runtime",runtimeProviderSaved) },
  "runtime.chat": {payload:noPayload,project:(result)=>inDomain(result,"runtime",runtimeChat)},
  "runtime.chat.configure": {payload:runtimeChatConfigure,project:(result)=>inDomain(result,"runtime",runtimeChat)},
  "runtime.providers": { payload: noPayload, project: (result) => inDomain(result,"runtime",runtimeProviders) },
  "runtime.models.status": { payload: noPayload, project: (result) => inDomain(result,"runtime",runtimeModels) },
  "runtime.models.engine.start": { payload: noPayload, project: (result) => inDomain(result,"runtime",runtimeModels) },
  "runtime.models.pull": { payload: runtimeModel, project: (result) => inDomain(result,"runtime",runtimeModels) },
  "runtime.models.cancel": { payload: noPayload, project: (result) => inDomain(result,"runtime",runtimeModels) },
  "runtime.models.remove": { payload: runtimeModel, project: (result) => inDomain(result,"runtime",runtimeModels) },
  "runtime.settings": { payload: noPayload, project: (result) => inDomain(result,"runtime",runtimeSettings) },
  "runtime.settings.set": { payload: runtimeSettingsSet, project: (result) => inDomain(result,"runtime",runtimeSettings) },
  "memory.status": {
    payload: noPayload,
    project: (result) => inDomain(result, "memory", memoryStatus),
  },
  "memory.stats": {
    payload: () => ({ action: SCORED_ACTION }),
    project: (result) => inDomain(result, "memory", memoryScore),
  },
  "memory.list": { payload: memoryListPayload, project: (result) => inDomain(result, "memory", memoryPage) },
  "memory.resolve": { payload: memoryResolvePayload, project: (result) => inDomain(result, "memory", memoryResolution) },
  "memory.get": { payload: memoryGetPayload, project: (result) => inDomain(result, "memory", memoryDetail) },
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
  // The orchestrator's own bound applies (20 per source); the screen chooses nothing.
  "meme.pool.refresh": {
    payload: () => ({}),
    project: (result) => inDomain(result, "meme", (body): MemePoolRefresh => ({
      added: count(body.added),
      sources: Array.isArray(body.scrapers) ? body.scrapers.filter((name): name is string => typeof name === "string").slice(0, 32).map((name) => name.slice(0, 80)) : [],
    })),
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
      ...Object.fromEntries(["source", "query", "from", "to"].filter(key => typeof raw[key] === "string" && (raw[key] as string).trim()).map(key => [key, (raw[key] as string).trim().slice(0, key === "source" || key === "query" ? 200 : 10)])),
    }),
    project: (result) => inDomain(result, "digest", digestPage),
    media: (projected) => isRecord(projected) && Array.isArray(projected.items)
      ? projected.items.filter(isRecord).map(item => text(item.mediaUrl)).filter(Boolean) : [],
  },
  "knowledge.ingest": {
    // A file travels as base64 text: the cap is the largest file, a third more.
    maxPayloadChars: 12_000_000,
    payload: (raw) => (raw.mode === "reading" ? readingIngest(raw) : buildIngest(raw)),
    project: (result) => inDomain(result, "knowledge", ingestResult),
  },
  "knowledge.list": {
    // The text of a document comes only one document at a time: it is what a document's tab shows,
    // and a page of twenty texts would be large for nothing.
    payload: (raw) => {
      // One document by its id, as a search hit names it: with its text, never a page of them.
      if (raw.source_id !== undefined) {
        if (typeof raw.source_id !== "string" || !INGEST_ID.test(raw.source_id)) return undefined;
        return { limit: 1, offset: 0, source_id: raw.source_id, ...(raw.include_text === true ? { include_text: true } : {}) };
      }
      const limit = clampInt(raw.limit, 1, 100, 20);
      return {
        limit,
        offset: clampInt(raw.offset, 0, 100_000, 0),
        ...(raw.include_text === true && limit === 1 ? { include_text: true } : {}),
        // what came from outside only: the documents Ningo keeps for itself are left out
        ...(raw.external === true ? { exclude_kinds: [...INTERNAL_KINDS] } : {}),
      };
    },
    project: (result) => inDomain(result, "knowledge", knowledgeDocuments),
  },
  "knowledge.classify": {
    payload: classificationPayload,
    writes: (wire) => "answers" in wire,
    project: (result) => inDomain(result, "knowledge", classificationResult),
  },
  "knowledge.delete": {
    // The id of one document, in the shape the service allows. It is what the question names.
    payload: (raw) => {
      const id = text(raw.id);
      return INGEST_ID.test(id) ? { source_id: id } : undefined;
    },
    project: (result) =>
      inDomain(result, "knowledge", (body): DeleteResult => ({ deleted: body.deleted === true })),
  },
  "knowledge.reindex": {
    // One small batch per click. A change needs a click just before it, so there is no loop to
    // run on the screen's own, and a long batch would keep one of the orchestrator's few request
    // workers busy. The service embeds 16 passages per call to the embedder.
    payload: (raw) => ({ limit: clampInt(raw.limit, 1, 64, 32) }),
    project: (result) => inDomain(result, "knowledge", reindexResult),
  },
};
