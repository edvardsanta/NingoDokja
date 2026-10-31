import test from "node:test";
import assert from "node:assert/strict";

import { ACTIONS } from "../src/main/actions.js";
import { isWriteAction, needsConfirmation } from "../src/shared/actions.js";
import { buildIngest } from "../src/main/ingest.js";

// Hand-built replies in the shapes the orchestrator uses, never a captured one: a real reply
// carries channel IDs and provider profiles.
const domainResult = {
  action: "inspect-ningo-platform",
  status: "degraded",
  services: {
    chat: { status: "error", error: "connection refused" },
    meme: { status: "ok", unsent_count: 4, enabled: true },
    scheduler: { status: "stopped", detail: "never announced", enabled: false },
    knowledge: "not an object",
  },
  channels: { meme: ["channel-1"], safe_only: ["channel-1"] },
  chat_profiles: { profiles: [{ name: "profile-1", base_url: "base-url-1", key_hint: "hint-1" }] },
  jobs: [{ name: "job-1", last_error: "job-error-1", announced_at: "announced-1", token: "job-token-1" }],
};

const compact = { event_id: "event-1", workflow: "ningo", domain: "system", result: domainResult };
const verbose = {
  event: { event_id: "event-1" },
  workflow: "ningo",
  domains: ["system"],
  result: { system: domainResult },
};

const project = ACTIONS["ningo.status"].project;

test("ningo.status keeps only what the health card reads", () => {
  const projected = project(compact);

  assert.deepEqual(projected, {
    status: "degraded",
    services: {
      chat: { status: "error", detail: "connection refused" },
      meme: { status: "ok", detail: "", enabled: true },
      scheduler: { status: "stopped", detail: "never announced", enabled: false },
    },
    // the card reads each job's switch, interval, last run and last error, and nothing else of it
    jobs: [{ name: "job-1", enabled: false, interval: "", intervalOverride: false, nextAt: "", lastAt: "", lastOutcome: "", lastError: "job-error-1" }],
  });

  const serialized = JSON.stringify(projected);
  for (const leaked of ["channel-1", "base-url-1", "hint-1", "announced-1", "job-token-1", "unsent_count"]) {
    assert.ok(!serialized.includes(leaked), `${leaked} must not reach the renderer`);
  }
});

test("ningo.status reads the verbose layout, where the result is nested by domain", () => {
  assert.deepEqual(project(verbose), project(compact));
});

test("ningo.status without services still answers", () => {
  const bare = { workflow: "ningo", domain: "system", result: {} };
  assert.deepEqual(project(bare), { status: "ok", services: {} });
});

test("ningo.status refuses a reply that is not the system domain's", () => {
  const other = { workflow: "ningo", domain: "memory", result: { services: {} } };
  for (const reply of [null, undefined, "text", [], {}, { result: "text" }, other]) {
    assert.throws(() => project(reply), /unexpected system reply/);
  }
});

test("ningo.status cuts a long error text instead of passing it all on", () => {
  const long = "x".repeat(500);
  const reply = {
    workflow: "ningo",
    domain: "system",
    result: { services: { meme: { status: "error", error: long } } },
  };

  const projected = project(reply) as { services: { meme: { detail: string } } };
  assert.equal(projected.services.meme.detail.length, 200);
  assert.ok(projected.services.meme.detail.endsWith("…"));
});

// ---- the other cards: hand-built replies in the compact layout, one domain each ----

const compactOf = (domain: string, result: Record<string, unknown>) => ({
  event_id: "event-1",
  workflow: domain,
  domain,
  result,
});

const memoryBody = {
  experiences: 40,
  pending: 12,
  resolved: 25,
  expired: 3,
  embedded: 38,
  needs_reindex: 2,
  embed_model: "model-1",
  embeddings: true,
  embedder_reachable: false,
  degraded: true,
};

test("memory.status keeps the counts and the state of similarity", () => {
  assert.deepEqual(ACTIONS["memory.status"].project(compactOf("memory", memoryBody)), {
    experiences: 40,
    pending: 12,
    resolved: 25,
    expired: 3,
    embedded: 38,
    needsReindex: 2,
    embedModel: "model-1",
    embeddings: true,
    embedderReachable: false,
  });
});

test("memory.stats keeps the score and never invents a number", () => {
  const score = {
    scored: 31,
    unscored: 4,
    min_scored: 30,
    brier_prediction: 0.081,
    brier_baseline: 0.27,
    skill: 0.7,
    beats_baseline: true,
    enough_data: true,
  };
  assert.deepEqual(ACTIONS["memory.stats"].project(compactOf("memory", score)), {
    scored: 31,
    unscored: 4,
    minScored: 30,
    brierPrediction: 0.081,
    brierBaseline: 0.27,
    skill: 0.7,
    beatsBaseline: true,
    enoughData: true,
  });
  assert.deepEqual(ACTIONS["memory.stats"].project(compactOf("memory", { scored: "many", skill: null })), {
    scored: 0,
    unscored: 0,
    minScored: 0,
    brierPrediction: 0,
    brierBaseline: 0,
    skill: 0,
    beatsBaseline: false,
    enoughData: false,
  });
});

test("a switched-off service projects to off, with its reason, for every action", () => {
  for (const [type, domain] of [
    ["memory.status", "memory"],
    ["memory.stats", "memory"],
    ["knowledge.status", "knowledge"],
    ["knowledge.search", "knowledge"],
    ["meme.status", "meme"],
    ["meme.list", "meme"],
    ["digest.status", "digest"],
    ["digest.items", "digest"],
  ] as const) {
    const reply = compactOf(domain, { skipped: true, reason: "switched off by the operator" });
    assert.deepEqual(ACTIONS[type].project(reply), { off: true, reason: "switched off by the operator" }, type);
  }
});

test("every action refuses a reply from another domain", () => {
  for (const type of ["memory.status", "knowledge.search", "meme.list", "digest.status", "digest.items"] as const) {
    assert.throws(() => ACTIONS[type].project(compactOf("system", {})), /unexpected .* reply/, type);
  }
});

test("knowledge.status keeps the counts and whether similarity answers", () => {
  const body = { documents: 6, chunks: 52, embedded: 50, pending_embeddings: 2, embed_model: "model-1", embedder_reachable: true, formats: [".md"] };
  assert.deepEqual(ACTIONS["knowledge.status"].project(compactOf("knowledge", body)), {
    documents: 6,
    chunks: 52,
    embedded: 50,
    pendingEmbeddings: 2,
    embedModel: "model-1",
    embedderReachable: true,
  });
  const unknown = ACTIONS["knowledge.status"].project(compactOf("knowledge", { documents: 1 })) as { embedderReachable: unknown };
  assert.equal(unknown.embedderReachable, null);
});

test("knowledge.search keeps what a result list shows and cuts long texts", () => {
  const body = {
    query: "free will",
    hits: [
      {
        rank: 1,
        title: "Document one",
        heading: "Section",
        kind: "note",
        source_ref: "ref-1",
        tags: ["a", "b", 7, ""],
        text: "t".repeat(900),
        score: 0.612345,
        coverage: 1,
        relevant: true,
        source_id: "note:one",
      },
      { rank: 2, title: "Document two", text: "short", score: null, relevant: false, source_id: "not an id; <b>" },
      "not a hit",
    ],
    relevant_count: 1,
    threshold: 0.45,
    degraded: true,
    reason: "similarity is down",
  };

  const projected = ACTIONS["knowledge.search"].project(compactOf("knowledge", body)) as {
    hits: Array<{ text: string; tags: string[]; relevant: boolean; score: number | null }>;
    relevantCount: number;
    degraded: boolean;
    reason: string;
  };

  assert.equal(projected.hits.length, 2);
  assert.equal(projected.hits[0]?.text.length, 400);
  assert.deepEqual(projected.hits[0]?.tags, ["a", "b"]);
  assert.equal(projected.hits[0]?.relevant, true);
  assert.equal(projected.hits[1]?.score, null);
  assert.equal(projected.hits[1]?.relevant, false);
  assert.equal(projected.relevantCount, 1);
  assert.equal(projected.degraded, true);
  assert.equal(projected.reason, "similarity is down");
  // A hit names its document so it can be opened, as the document list already does; anything
  // that is not an id is dropped.
  assert.equal((projected.hits[0] as { sourceId?: string }).sourceId, "note:one");
  assert.equal((projected.hits[1] as { sourceId?: string }).sourceId, "");
});

test("knowledge.list reads one document by its id, with its text, and nothing else", () => {
  const payload = ACTIONS["knowledge.list"].payload;
  assert.deepEqual(payload({ source_id: "note:one", include_text: true, limit: 50, offset: 9, kind: "x" }), { limit: 1, offset: 0, source_id: "note:one", include_text: true });
  assert.deepEqual(payload({ source_id: "note:one" }), { limit: 1, offset: 0, source_id: "note:one" });
  for (const source_id of ["", "a b", "x".repeat(201), 3, "' OR 1=1 --"]) assert.equal(payload({ source_id }), undefined, String(source_id));
});

test("meme.status keeps the queue counts", () => {
  assert.deepEqual(
    ACTIONS["meme.status"].project(compactOf("meme", { status: "ok", unsent_count: 14, sent_count: 220, pool: "x" })),
    { status: "ok", unsent: 14, sent: 220 },
  );
});

test("meme.list keeps the page and tells which pictures and videos the screen may preview", () => {
  const body = {
    total: 30,
    offset: 12,
    memes: [
      { url: "https://images.example/a.png", title: "A", source: "source-1", tags: "x, y", sent_count: 0, date_created: "2026-09-01T10:00:00", date_sent: null },
      { url: "https://images.example/b.gif", title: "B", source: "source-2", tags: ["p", "q"], date_created: "2026-09-02T10:00:00", date_sent: "2026-09-03T10:00:00" },
      { title: "no address" },
      42,
    ],
  };
  const action = ACTIONS["meme.list"];
  const projected = action.project(compactOf("meme", body));

  assert.deepEqual(projected, {
    total: 30,
    offset: 12,
    memes: [
      { url: "https://images.example/a.png", title: "A", tags: "x, y", source: "source-1", createdAt: "2026-09-01T10:00:00", sentAt: "" },
      { url: "https://images.example/b.gif", title: "B", tags: "p, q", source: "source-2", createdAt: "2026-09-02T10:00:00", sentAt: "2026-09-03T10:00:00" },
      { url: "", title: "no address", tags: "", source: "", createdAt: "", sentAt: "" },
    ],
  });
  assert.deepEqual(action.media?.(projected), ["https://images.example/a.png", "https://images.example/b.gif"]);
});

test("digest.status keeps the sources and how each is doing, and nothing the service did not say", () => {
  const body = {
    configured: true,
    directory_error: "",
    ok: 1, failed: 1, pending: 0, disabled: 1, invalid: 1, items: 3, enabled: 2,
    plugins: [
      { id: "source-1", name: "First source", state: "ok", running: true, items: 3, skipped: 1, last_ok: "2026-10-02T10:00:00Z", last_error: "", interval_seconds: 300, command: ["/secret/path"] },
      { id: "source-2", name: "", state: "failed", running: false, items: 0, skipped: 0, last_ok: "", last_error: "exit status 2" },
      { id: "source-3", name: "Third", state: "something new", last_error: "" },
      { name: "no id" },
      "not an object",
    ],
  };
  assert.deepEqual(ACTIONS["digest.status"].project(compactOf("digest", body)), {
    configured: true,
    directoryError: "",
    ok: 1, failed: 1, pending: 0, disabled: 1, invalid: 1, items: 3,
    sources: [
      { id: "source-1", name: "First source", state: "ok", running: true, items: 3, skipped: 1, lastOk: "2026-10-02T10:00:00Z", error: "" },
      { id: "source-2", name: "source-2", state: "failed", running: false, items: 0, skipped: 0, lastOk: "", error: "exit status 2" },
      { id: "source-3", name: "Third", state: "unknown", running: false, items: 0, skipped: 0, lastOk: "", error: "" },
      { id: "", name: "no id", state: "unknown", running: false, items: 0, skipped: 0, lastOk: "", error: "" },
    ],
  });
});

test("digest.status cuts long texts and caps the number of sources", () => {
  const long = "x".repeat(500);
  const plugins = Array.from({ length: 80 }, (_, i) => ({ id: `source-${i}`, name: long, state: "ok", last_error: long }));
  const projected = ACTIONS["digest.status"].project(compactOf("digest", { configured: true, plugins, directory_error: long })) as {
    sources: Array<{ name: string; error: string }>;
    directoryError: string;
  };
  assert.equal(projected.sources.length, 50);
  assert.ok(projected.sources[0]!.name.length <= 80 && projected.sources[0]!.error.length <= 200);
  assert.ok(projected.directoryError.length <= 200);
});

test("digest.status without sources still answers", () => {
  assert.deepEqual(ACTIONS["digest.status"].project(compactOf("digest", { configured: false })), {
    configured: false, directoryError: "", ok: 0, failed: 0, pending: 0, disabled: 0, invalid: 0, items: 0, sources: [],
  });
});

test("digest.items keeps the page and never hands the screen an address or the plugin", () => {
  const body = {
    items: [
      { id: "1", plugin: "source-1", title: "A title", summary: "Some text", url: "https://example.com/1", published: "2026-10-02T09:00:00Z", source: "Example" },
      { id: "2", title: "No other fields" },
      { title: "" , id: "empty title still passes through here"},
      7,
    ],
    total: 20, offset: 0, more: 17, updated: "2026-10-02T10:00:00Z", duplicates: 4, older: 2, sources: 3,
  };
  const projected = ACTIONS["digest.items"].project(compactOf("digest", body));
  assert.deepEqual(projected, {
    items: [
      { id: "1", title: "A title", summary: "Some text", source: "Example", published: "2026-10-02T09:00:00Z" },
      { id: "2", title: "No other fields", summary: "", source: "", published: "" },
      { id: "empty title still passes through here", title: "", summary: "", source: "", published: "" },
    ],
    total: 20, offset: 0, more: 17, updated: "2026-10-02T10:00:00Z",
  });
  assert.doesNotMatch(JSON.stringify(projected), /example\.com\/1|source-1/);
  assert.deepEqual(ACTIONS["digest.items"].media!(projected), [], "article addresses are never authorized as media");
});

test("digest.items keeps up to three prompts the source wrote, and none when it wrote none", () => {
  const projected = ACTIONS["digest.items"].project(compactOf("digest", { items: [
    { id: "1", title: "With prompts", prompts: [" What does it teach? ", "", 5, "Second", "Third", "Fourth"] },
    { id: "2", title: "Not a list", prompts: "What?" },
  ] })) as { items: Array<{ prompts?: string[] }> };
  assert.deepEqual(projected.items[0]!.prompts, ["What does it teach?", "Second", "Third"]);
  assert.equal("prompts" in projected.items[1]!, false);
});

test("digest.items cuts long texts", () => {
  const long = "x".repeat(2000);
  const projected = ACTIONS["digest.items"].project(compactOf("digest", { items: [{ id: long, title: long, summary: long, source: long }] })) as {
    items: Array<{ id: string; title: string; summary: string; source: string }>;
  };
  const [item] = projected.items;
  assert.ok(item!.id.length <= 120 && item!.title.length <= 300 && item!.summary.length <= 600 && item!.source.length <= 80);
});

test("knowledge.list asks for a document's text only one document at a time", () => {
  const payload = ACTIONS["knowledge.list"].payload;
  assert.deepEqual(payload({ limit: 1, offset: 7, include_text: true }), { limit: 1, offset: 7, include_text: true });
  // a page of documents never carries their texts, and only a real true asks for one
  assert.deepEqual(payload({ limit: 20, offset: 0, include_text: true }), { limit: 20, offset: 0 });
  assert.deepEqual(payload({ limit: 1, offset: 0, include_text: "yes" }), { limit: 1, offset: 0 });
  assert.deepEqual(payload({ limit: 1, offset: 0, include_text: true, kind: "meme_hashtag", extra: "x" }), { limit: 1, offset: 0, include_text: true });
});

test("knowledge.list keeps a document's text, cut when it is too long, and leaves out what it does not show", () => {
  const project = ACTIONS["knowledge.list"].project;
  const row = { source_id: "note:a-1", title: "A", kind: "note", source_ref: "", tags: [], chunks: 2, embedded: 2, updated_at: "2026-10-02T10:00:00", content_sha: "hash-1" };
  const listed = project(compactOf("knowledge", { documents: [row], total: 1, offset: 0 })) as { documents: Array<Record<string, unknown>> };
  assert.equal("text" in (listed.documents[0] ?? {}), false);
  assert.equal("content_sha" in (listed.documents[0] ?? {}), false);

  const short = project(compactOf("knowledge", { documents: [{ ...row, text: "Two passages." }], total: 1, offset: 0 })) as { documents: Array<Record<string, unknown>> };
  assert.equal(short.documents[0]?.text, "Two passages.");
  assert.equal(short.documents[0]?.truncated, false);

  const long = project(compactOf("knowledge", { documents: [{ ...row, text: "x".repeat(250_000) }], total: 1, offset: 0 })) as { documents: Array<{ text: string; truncated: boolean }> };
  assert.equal(long.documents[0]?.text.length, 200_000);
  assert.equal(long.documents[0]?.truncated, true);
});

test("knowledge.ingest says what a single document did, in the same counts as many", () => {
  const project = ACTIONS["knowledge.ingest"].project;
  assert.deepEqual(
    project(compactOf("knowledge", { source_id: "note:a-1", created: true, changed: true, chunks: 3, embedded: 3, degraded: false })),
    { count: 1, created: 1, updated: 0, unchanged: 0, chunks: 3, degraded: false, reason: "" },
  );
  assert.deepEqual(
    project(compactOf("knowledge", { source_id: "url:x", created: false, changed: true, chunks: 2 })),
    { count: 1, created: 0, updated: 1, unchanged: 0, chunks: 2, degraded: false, reason: "" },
  );
  assert.deepEqual(
    project(compactOf("knowledge", { source_id: "note:a-1", created: false, changed: false, pending_embeddings: 0 })),
    { count: 1, created: 0, updated: 0, unchanged: 1, chunks: 0, degraded: false, reason: "" },
  );
  assert.deepEqual(
    project(compactOf("knowledge", {
      documents: [{ created: true, changed: true }, { created: true, changed: true }, { created: false, changed: true }, { created: false, changed: false }],
      count: 4, created: 2, unchanged: 1, chunks: 9, embedded: 0, degraded: true, reason: "no embedding server is configured",
    })),
    { count: 4, created: 2, updated: 1, unchanged: 1, chunks: 9, degraded: true, reason: "no embedding server is configured" },
  );
});

test("knowledge.ingest cuts a long reason and is a change", () => {
  const projected = ACTIONS["knowledge.ingest"].project(compactOf("knowledge", { created: true, changed: true, degraded: true, reason: "x".repeat(500) })) as { reason: string };
  assert.ok(projected.reason.length <= 200);
});

test("ningo.status keeps an interface's kind and its state, and drops anything else it reports", () => {
  const reply = {
    workflow: "ningo",
    domain: "system",
    result: {
      services: {
        whatsapp: {
          status: "error",
          kind: "interface",
          state: "waiting_for_link",
          detail: "the last link code expired",
          enabled: true,
          link_code: "secret-code-1",
          chat: "chat-id-1",
        },
        discord: { status: "ok", kind: "interface", state: "NOT A NAME", detail: "" },
        meme: { status: "ok", kind: "something-else", state: "connected" },
      },
    },
  };

  const projected = project(reply) as { services: Record<string, unknown> };

  assert.deepEqual(projected.services, {
    whatsapp: {
      status: "error",
      detail: "the last link code expired",
      enabled: true,
      kind: "interface",
      state: "waiting_for_link",
    },
    discord: { status: "ok", detail: "", kind: "interface" },
    meme: { status: "ok", detail: "" },
  });
  const serialized = JSON.stringify(projected);
  for (const leaked of ["secret-code-1", "chat-id-1", "NOT A NAME"]) {
    assert.ok(!serialized.includes(leaked), `${leaked} must not reach the renderer`);
  }
});

// ---- linking an interface ----

const linkStatus = ACTIONS["communications.link.status"];
const linkReset = ACTIONS["communications.link.reset"];
const square = (side: number) => Array.from({ length: side }, (_, y) => Array.from({ length: side }, (_, x) => ((x + y) % 2 ? "1" : "0")).join(""));

test("the link actions send only the interface, fixed, whatever the screen adds", () => {
  for (const action of ["communications.link.status", "communications.link.start", "communications.link.reset"] as const) {
    const { payload } = ACTIONS[action];
    assert.deepEqual(payload({ interface: "whatsapp", name: "x", token: "t", enabled: true }), { interface: "whatsapp" });
    assert.equal(payload({ interface: "discord" }), undefined);
    assert.equal(payload({ interface: "whatsapp2" }), undefined);
    assert.equal(payload({}), undefined);
  }
});

test("a link reply keeps the state and the picture, never the rows or the interface's own fields", () => {
  const reply = {
    workflow: "interface-link",
    domain: "system",
    result: { interface: "whatsapp", state: "waiting_for_link", matrix: square(25), code: "2@secret-code", chat: "chat-id-1" },
  };

  const projected = linkStatus.project(reply) as { state: string; image?: string };

  assert.deepEqual(Object.keys(projected).sort(), ["image", "state"]);
  assert.equal(projected.state, "waiting_for_link");
  assert.ok(projected.image?.startsWith("data:image/svg+xml;base64,"));
  const serialized = JSON.stringify(projected);
  for (const leaked of ["secret-code", "chat-id-1", square(25)[0] ?? "no-row"]) {
    assert.ok(!serialized.includes(leaked), `${leaked} must not reach the renderer`);
  }
});

test("a link reply without a code, or with a bad one, has no picture", () => {
  const wrap = (result: Record<string, unknown>) => ({ workflow: "interface-link", domain: "system", result });

  assert.deepEqual(linkStatus.project(wrap({ state: "connected" })), { state: "connected" });
  assert.deepEqual(linkStatus.project(wrap({ state: "waiting_for_link", matrix: ["<svg onload=x>"] })), { state: "waiting_for_link" });
  assert.deepEqual(linkReset.project(wrap({ state: "NOT A NAME" })), { state: "unknown" });
});

// ---- conversations an interface keeps ----

const history = ACTIONS["communications.history"];
const channels = ACTIONS["communications.channels"];

test("history accepts a Discord channel id or a WhatsApp alias, and nothing that could name a path", () => {
  assert.deepEqual(history.payload({ channel_id: "123456789012345678", before: "55" }), { channel_id: "123456789012345678", before: "55" });
  assert.deepEqual(history.payload({ channel_id: "whatsapp:friend", before: "12" }), { channel_id: "whatsapp:friend", before: "12" });
  assert.deepEqual(history.payload({ channel_id: "whatsapp:team-chat_2" }), { channel_id: "whatsapp:team-chat_2", before: "" });
  for (const channel_id of ["whatsapp:", "whatsapp:../friend", "whatsapp:Friend", "whatsapp:a b", "whatsapp:" + "x".repeat(40), "discord:123", "friend", "../1"]) {
    assert.equal(history.payload({ channel_id }), undefined, channel_id);
  }
  assert.equal(history.payload({ channel_id: "whatsapp:friend", before: "1;drop" }), undefined);
});

test("the channel list keeps each Discord channel's server and how many servers could not be read", () => {
  const reply = {
    workflow: "communications",
    domain: "communications",
    result: {
      channels: [
        { id: "11", name: "chat", guild_id: "10", guild_name: "resenhudos" },
        { id: "12", name: "odd", guild_id: "../x", guild_name: "bad" },
      ],
      unavailable_guilds: ["20", "30"],
    },
  };
  const projected = channels.project(reply as never) as { channels: Record<string, unknown>[]; unavailableServers?: number };
  assert.deepEqual(projected.channels[0], { id: "11", name: "chat", guildId: "10", guildName: "resenhudos" });
  assert.deepEqual(projected.channels[1], { id: "12", name: "odd" });
  assert.equal(projected.unavailableServers, 2);
});

test("the channel list keeps the retention of a WhatsApp conversation and nothing else it reports", () => {
  const reply = {
    workflow: "communications",
    domain: "communications",
    result: {
      channels: [
        { id: "123", name: "reading-room" },
        { id: "whatsapp:friend", name: "friend", kind: "direct", retention_days: 30, number: "5511987654321", jid: "x@s.whatsapp.net" },
        { id: "whatsapp:odd", name: "odd", retention_days: 99999 },
      ],
      unavailable: ["whatsapp"],
    },
  };

  const projected = channels.project(reply) as { channels: Array<Record<string, unknown>> };

  assert.deepEqual(projected.channels, [
    { id: "123", name: "reading-room" },
    { id: "whatsapp:friend", name: "friend", retentionDays: 30 },
    { id: "whatsapp:odd", name: "odd", retentionDays: 365 },
  ]);
  assert.ok(!JSON.stringify(projected).includes("5511987654321"));
});

// ---- sending to a WhatsApp conversation ----

const send = ACTIONS["communications.send"];

test("a send to a WhatsApp alias is text only, and the alias is the only thing that names the chat", () => {
  assert.deepEqual(send.payload({ channel_id: "whatsapp:friend", content: "  hello  " }), {
    channel_ids: ["whatsapp:friend"],
    content: "hello",
  });
  for (const raw of [
    { channel_id: "whatsapp:friend", content: "hi", attachment_url: "https://example.com/meme.jpg" },
    { channel_id: "whatsapp:friend", content: "   " },
    { channel_id: "whatsapp:friend", content: "x".repeat(2001) },
    { channel_id: "whatsapp:../friend", content: "hi" },
    { channel_id: "whatsapp:Friend", content: "hi" },
    { channel_id: "5511987654321@s.whatsapp.net", content: "hi" },
  ]) {
    assert.equal(send.payload(raw), undefined, JSON.stringify(raw).slice(0, 60));
  }
});

test("a Discord send is built as before", () => {
  assert.deepEqual(send.payload({ channel_id: "123456789", content: "hi", attachment_url: "https://example.com/a.jpg" }), {
    channel_ids: ["123456789"],
    content: "hi",
    attachment_url: "https://example.com/a.jpg",
  });
  assert.deepEqual(send.payload({ channel_id: "123456789", content: "hi" }), { channel_ids: ["123456789"], content: "hi" });
});

test("interface setup state survives projection without exposing its endpoint",()=>{
 const result=ACTIONS["ningo.status"].project({domain:"system",result:{status:"ok",services:{whatsapp:{kind:"interface",status:"unchecked",state:"not_configured",configured:false,endpoint:"http://private.example/status",enabled:true}}}}) as {services:Record<string,unknown>};
 assert.deepEqual(result.services.whatsapp,{kind:"interface",status:"unchecked",state:"not_configured",configured:false,enabled:true,detail:""});
});

test("digest.status exposes processing state without plugin commands or environment", () => {
  const result = ACTIONS["digest.status"].project({ domain: "digest", result: {
    configured: true, plugins: [], providers: [{
      id: "media-library", name: "Media library", state: "failed", enabled: true,
      last_error: "cache unavailable", last_ok: "", command: ["private-command"], env: ["PRIVATE_SETTING"],
    }],
  } });
  assert.deepEqual((result as {providers:unknown[]}).providers, [{
    id: "media-library", name: "Media library", state: "failed", running: false,
    items: 0, skipped: 0, lastOk: "", error: "cache unavailable",
  }]);
  assert.equal(JSON.stringify(result).includes("private-command"), false);
  assert.equal(JSON.stringify(result).includes("PRIVATE_SETTING"), false);
});

test("a reading is added to the Library by its id, from the page the shell kept, never from the screen", () => {
  ACTIONS["digest.items"].project(compactOf("digest", {
    items: [
      { id: "kept-1", title: "Kept", url: "https://example.com/kept" },
      { id: "odd-1", title: "Odd", url: "javascript:alert(1)" },
      { id: "none-1", title: "No page" },
    ],
    total: 3, offset: 0, more: 0, updated: "",
  }));
  const payload = ACTIONS["knowledge.ingest"].payload;
  assert.deepEqual(payload({ mode: "reading", reading: "kept-1" }), buildIngest({ mode: "address", address: "https://example.com/kept" }));
  assert.equal(payload({ mode: "reading", reading: "odd-1" }), undefined, "only a web page is kept");
  assert.equal(payload({ mode: "reading", reading: "none-1" }), undefined);
  assert.equal(payload({ mode: "reading", reading: "never-shown" }), undefined);
  assert.deepEqual(
    payload({ mode: "reading", reading: "kept-1", address: "https://example.com/other" }),
    payload({ mode: "reading", reading: "kept-1" }),
    "an address from the screen is ignored",
  );
});

test("knowledge.list leaves out what Ningo keeps for himself only when asked, and the kinds are the shell's", () => {
  const payload = ACTIONS["knowledge.list"].payload;
  assert.deepEqual(payload({ limit: 20, offset: 0 }), { limit: 20, offset: 0 });
  assert.deepEqual(payload({ limit: 20, offset: 0, external: true }), { limit: 20, offset: 0, exclude_kinds: ["meme_hashtag"] });
  assert.deepEqual(payload({ limit: 20, offset: 0, exclude_kinds: ["note"] }), { limit: 20, offset: 0 }, "the screen cannot name kinds");
});


test("meme.hashtag.tag may carry the meme's text the owner wrote, bounded", () => {
  const payload = ACTIONS["meme.hashtag.tag"].payload;
  assert.deepEqual(payload({ url: "https://images.example/a.png", hashtag: "#x", text: "  words  " }), { url: "https://images.example/a.png", hashtag: "#x", text: "words" });
  assert.deepEqual(payload({ url: "https://images.example/a.png", hashtag: "#x", text: "   " }), { url: "https://images.example/a.png", hashtag: "#x" });
  assert.equal(payload({ url: "https://images.example/a.png", hashtag: "#x", text: "w".repeat(2001) }), undefined);
});

test("a turn carries the documents its reply cites, bounded, and only those that name one", () => {
  const project = ACTIONS["conversation.read"].project;
  const id = "0".repeat(32);
  const turn = (sources: unknown) => project({ domain: "conversation", result: {
    conversation: { id, kind: "general", revision: 1 },
    turns: [{ id, seq: 1, send_id: "send-0001", content: "q", reply: "a", state: "completed", sources }],
  } }) as { turns: { sources?: unknown[] }[] };
  const kept = { number: 1, source_id: "note:one", title: "One", reference: "https://example.com/a", excerpt: "x".repeat(900), secret: "drop" };
  const [source] = turn([kept, { number: 2, title: "names nothing" }, "junk"]).turns[0]!.sources as { id: string; excerpt: string }[];
  assert.equal(source!.id, "note:one");
  assert.ok(source!.excerpt.length <= 601);
  assert.equal("secret" in source!, false);
  assert.equal(turn([kept, { number: 2, title: "names nothing" }]).turns[0]!.sources!.length, 1);
  assert.equal("sources" in turn(undefined).turns[0]!, false, "no sources, no field");
});

test("meme.hashtag.suggest is a bounded read of one meme, and keeps only its answer", () => {
  const action = ACTIONS["meme.hashtag.suggest"];
  assert.equal(isWriteAction("meme.hashtag.suggest"), false);
  assert.deepEqual(action.payload({ url: " https://images.example/a.png ", min_score: 0, text: "x" }), { url: "https://images.example/a.png" });
  assert.equal(action.payload({ url: "file:///etc/hosts" }), undefined);
  assert.equal(action.payload({ url: `https://images.example/${"a".repeat(2048)}` }), undefined);
  const projected = action.project({ domain: "meme", result: { relevant: true, hashtag: "#x", score: 0.9, matched_text: "private" } });
  assert.deepEqual(projected, { relevant: true, hashtag: "#x" });
  assert.deepEqual(action.project({ domain: "meme", result: { skipped: true, reason: "paused" } }), { off: true, reason: "paused" });
});

test("meme.pool.refresh is a change the screen cannot size, and keeps only counts and source names", () => {
  assert.equal(isWriteAction("meme.pool.refresh"), true);
  assert.equal(needsConfirmation("meme.pool.refresh"), false);
  assert.deepEqual(ACTIONS["meme.pool.refresh"].payload({ max_items_per_scraper: 200, scope: "all" }), {});
  const projected = ACTIONS["meme.pool.refresh"].project({ domain: "meme", result: { added: 4, scrapers: ["a", 7, "b"], collection: { secret: "drop" }, status: "refreshed" } });
  assert.deepEqual(projected, { added: 4, sources: ["a", "b"] });
});


test("saving a collected reading uses its full text and keeps a stable identity per day", () => {
  const project = ACTIONS["digest.items"].project;
  const ingest = ACTIONS["knowledge.ingest"].payload;
  const body = "First paragraph.\n\n" + "Full reading text. ".repeat(100);
  const reading = { id: "daily:2026-10-27", plugin: "daily", title: "Daily reading", summary: "Excerpt", body, url: "https://example.com/" };
  const shown = project(compactOf("digest", { items: [reading] }));
  assert.equal((shown as { items: Array<{ previewText: string }> }).items[0]!.previewText, body.trim());
  const payload = ingest({ mode: "reading", reading: reading.id, body: "Forged text", source_id: "forged" })!;
  assert.equal(payload.body, body.trim());
  assert.equal(payload.title, reading.title);
  assert.equal(payload.kind, "article");
  assert.equal(payload.source_ref, reading.url);
  assert.equal(payload.source, undefined, "the empty application homepage is never fetched");
  assert.match(String(payload.source_id), /^reading:[a-f0-9]{64}$/);
  project(compactOf("digest", { items: [{ ...reading, body: "Revised full text" }] }));
  const revised = ingest({ mode: "reading", reading: reading.id })!;
  assert.equal(revised.source_id, payload.source_id);
  assert.equal(revised.body, "Revised full text");
  project(compactOf("digest", { items: [{ ...reading, id: "daily:2026-10-28" }] }));
  assert.notEqual(ingest({ mode: "reading", reading: "daily:2026-10-28" })!.source_id, payload.source_id);
});

test("reading snapshots support missing links and discard stale text on refresh", () => {
  const project = ACTIONS["digest.items"].project;
  const ingest = ACTIONS["knowledge.ingest"].payload;
  const reading = { id: "snapshot-only", title: "Snapshot", body: "Collected full text" };
  project(compactOf("digest", { items: [reading] }));
  assert.equal(ingest({ mode: "reading", reading: reading.id })!.body, reading.body);
  project(compactOf("digest", { items: [{ ...reading, body: "", url: "https://example.com/page" }] }));
  assert.deepEqual(ingest({ mode: "reading", reading: reading.id }), buildIngest({ mode: "address", address: "https://example.com/page" }));
  project(compactOf("digest", { items: [{ ...reading, body: "" }] }));
  assert.equal(ingest({ mode: "reading", reading: reading.id }), undefined);
});


test("digest preview exposes bounded text and authorizes only valid source media", () => {
  const projected = ACTIONS["digest.items"].project(compactOf("digest", { items: [
    { id: "picture", title: "Picture", body: "x".repeat(120_000), media_url: "https://example.com/picture.jpg", url: "https://example.com/article", secret: "PRIVATE" },
    { id: "bad", title: "Bad", media_url: "javascript:alert(1)" },
    { id: "credentials", title: "Credentials", media_url: "https://user:pass@example.com/image.jpg" },
  ] })) as { items: Array<Record<string, unknown>> };
  assert.equal(String(projected.items[0]!.previewText).length, 100_000);
  assert.deepEqual(ACTIONS["digest.items"].media!(projected), ["https://example.com/picture.jpg"]);
  assert.equal(projected.items[1]!.mediaUrl, undefined);
  assert.equal(projected.items[2]!.mediaUrl, undefined);
  assert.equal(JSON.stringify(projected).includes("PRIVATE"), false);
});

test("digest filters are allowlisted and empty filters preserve the existing request", () => {
  const build = ACTIONS["digest.items"].payload;
  assert.deepEqual(build({ source: " alpha ", query: " reading ", from: "2026-10-01", to: "2026-10-27", extra: "ignored" }), {
    limit: 8, offset: 0, source: "alpha", query: "reading", from: "2026-10-01", to: "2026-10-27",
  });
  assert.deepEqual(build({ source: "", query: " ", from: "", to: "" }), { limit: 8, offset: 0 });
});
