import test from "node:test";
import assert from "node:assert/strict";

import { ACTIONS } from "../src/main/actions.js";

// Hand-built replies in the shapes the orchestrator uses, never a captured one: a real reply
// carries channel IDs and provider profiles.
const domainResult = {
  action: "inspect-ningo-platform",
  status: "degraded",
  services: {
    chat_ai: { status: "error", error: "connection refused" },
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
      chat_ai: { status: "error", detail: "connection refused" },
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
        source_id: "id-must-not-leak",
      },
      { rank: 2, title: "Document two", text: "short", score: null, relevant: false },
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
  assert.ok(!JSON.stringify(projected).includes("id-must-not-leak"));
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
  assert.equal(ACTIONS["digest.items"].media, undefined, "a digest item has nothing to preview");
});

test("digest.items cuts long texts", () => {
  const long = "x".repeat(2000);
  const projected = ACTIONS["digest.items"].project(compactOf("digest", { items: [{ id: long, title: long, summary: long, source: long }] })) as {
    items: Array<{ id: string; title: string; summary: string; source: string }>;
  };
  const [item] = projected.items;
  assert.ok(item!.id.length <= 120 && item!.title.length <= 300 && item!.summary.length <= 600 && item!.source.length <= 80);
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
