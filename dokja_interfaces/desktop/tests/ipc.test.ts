import test from "node:test";
import assert from "node:assert/strict";

import { handleRequest, isAllowedAction } from "../src/main/ipc.js";
import {
  OrchestratorClient,
  OrchestratorError,
  type OrchestratorReply,
} from "../src/main/orchestrator_client.js";
import { ok, startReplyServer } from "./support/reply_server.js";

const limits = { defaultTimeoutMs: 15_000, maxTimeoutMs: 60_000 };

const compactStatus = {
  workflow: "ningo",
  domain: "system",
  result: { status: "ok", services: { meme: { status: "ok" } } },
};

// A stand-in for the orchestrator client that records what reaches it.
function recorder(reply: OrchestratorReply | Error) {
  const calls: Array<{ type: string; payload: Record<string, unknown>; timeoutMs: number }> = [];
  return {
    calls,
    orchestrator: {
      async request(type: string, payload: Record<string, unknown>, timeoutMs: number) {
        calls.push({ type, payload, timeoutMs });
        if (reply instanceof Error) throw reply;
        return reply;
      },
    },
  };
}

function failure(result: Awaited<ReturnType<typeof handleRequest>>) {
  assert.equal(result.ok, false);
  assert.ok(!result.ok);
  return result.error;
}

test("refuses every action outside the allow-list before touching the orchestrator", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: compactStatus });
  const refused = [
    "services.set ",
    "scheduler.jobs.announce",
    "discord.send",
    "meme.dispatch.scheduled",
    "chat.profile.use",
    "memory.forget",
    "memory.record",
    "memory.resolve",
    "knowledge.add",
    "knowledge.search ",
    "knowledge.ingest\n",
    "book.resource.classify",
    "message.created",
    "NINGO.STATUS",
    "ningo.status ",
    " ningo.status",
    "ningo.status\n",
    "ningo.*",
    "__proto__",
    "constructor",
    "",
    42,
    null,
    undefined,
    {},
    ["ningo.status"],
  ];

  for (const type of refused) {
    const error = failure(await handleRequest({ type }, orchestrator, limits));
    assert.equal(error.code, "denied", `type ${JSON.stringify(type)}`);
  }
  assert.equal(calls.length, 0, "the orchestrator must never see a refused request");
});

test("refuses a request that is not an object", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: compactStatus });
  for (const raw of [null, undefined, "ningo.status", 7, ["ningo.status"]]) {
    assert.equal(failure(await handleRequest(raw, orchestrator, limits)).code, "invalid");
  }
  assert.equal(calls.length, 0);
});

test("allows ningo.status and hands back only its projection", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: compactStatus });
  const result = await handleRequest({ type: "ningo.status" }, orchestrator, limits);

  assert.deepEqual(result, {
    ok: true,
    result: { status: "ok", services: { meme: { status: "ok", detail: "" } } },
  });
  assert.deepEqual(calls, [{ type: "ningo.status", payload: {}, timeoutMs: 15_000 }]);
});

test("only a small plain-object payload is forwarded", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: compactStatus });
  const cycle: Record<string, unknown> = {};
  cycle.self = cycle;

  for (const payload of [["a"], "text", 5, cycle, { big: "x".repeat(20_000) }]) {
    const error = failure(await handleRequest({ type: "ningo.status", payload }, orchestrator, limits));
    assert.equal(error.code, "invalid");
  }
  assert.equal(calls.length, 0);

  await handleRequest({ type: "ningo.status", payload: { limit: 3 } }, orchestrator, limits);
  assert.deepEqual(calls[0]?.payload, {}, "an action without parameters sends none");
});

test("each action builds its own payload, so the screen cannot add fields", async () => {
  const sent = async (type: string, payload: unknown) => {
    const { orchestrator, calls } = recorder({ status: "ok", result: {} });
    await handleRequest({ type, payload }, orchestrator, limits);
    return calls[0]?.payload;
  };

  assert.deepEqual(
    await sent("knowledge.search", { query: "  free will  ", k: 99, min_score: 0, source: "x" }),
    { query: "free will", k: 10 },
  );
  assert.deepEqual(await sent("knowledge.search", { query: "free will" }), { query: "free will", k: 5 });
  assert.deepEqual(await sent("memory.stats", { action: "something.else" }), { action: "hashtag.suggest" });
  assert.deepEqual(await sent("meme.list", {}), { scope: "unsent", limit: 12, offset: 0 });
  assert.deepEqual(await sent("meme.list", { scope: "sent", limit: 500, offset: -4, nsfw: false }), {
    scope: "sent",
    limit: 24,
    offset: 0,
  });
  assert.deepEqual(await sent("meme.list", { scope: "../etc", limit: "many" }), {
    scope: "unsent",
    limit: 12,
    offset: 0,
  });
  assert.deepEqual(await sent("digest.status", { refresh: true }), {}, "digest.status takes no parameters");
  assert.deepEqual(await sent("digest.items", {}), { limit: 8, offset: 0 });
  assert.deepEqual(
    await sent("digest.items", { limit: 500, offset: -3, per_source: 10, max_age_hours: 9999, refresh: true }),
    { limit: 50, offset: 0 },
    "the screen cannot change the digest's rules",
  );
  assert.deepEqual(await sent("digest.items", { limit: 16, offset: 2000 }), { limit: 16, offset: 1000 });
});

test("a search needs a query of a sensible size", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: {} });
  for (const payload of [{}, { query: "" }, { query: "   " }, { query: 7 }, { query: "x".repeat(501) }]) {
    const error = failure(await handleRequest({ type: "knowledge.search", payload }, orchestrator, limits));
    assert.equal(error.code, "invalid", JSON.stringify(payload).slice(0, 40));
  }
  assert.equal(calls.length, 0);
});

test("a switched-off service is an answer, not a failure", async () => {
  const { orchestrator } = recorder({
    status: "ok",
    result: { workflow: "memory", domain: "memory", result: { skipped: true, reason: "switched off by the operator" } },
  });

  assert.deepEqual(await handleRequest({ type: "memory.status" }, orchestrator, limits), {
    ok: true,
    result: { off: true, reason: "switched off by the operator" },
  });
});

test("the image addresses of a meme page are handed to the gate", async () => {
  const remembered: string[][] = [];
  const { orchestrator } = recorder({
    status: "ok",
    result: {
      workflow: "meme",
      domain: "meme",
      result: { total: 2, offset: 0, memes: [{ url: "https://images.example/a.png" }, { url: "https://images.example/b.gif" }] },
    },
  });

  await handleRequest({ type: "meme.list" }, orchestrator, limits, { remember: (urls) => remembered.push(urls) });
  assert.deepEqual(remembered, [["https://images.example/a.png", "https://images.example/b.gif"]]);

  const status = recorder({ status: "ok", result: compactStatus });
  await handleRequest({ type: "ningo.status" }, status.orchestrator, limits, { remember: (urls) => remembered.push(urls) });
  assert.equal(remembered.length, 1, "an action without media does not touch the gate");
});

test("the timeout is the one asked for, clamped to the limits", async () => {
  const asked = async (options: unknown) => {
    const { orchestrator, calls } = recorder({ status: "ok", result: compactStatus });
    await handleRequest({ type: "ningo.status", options }, orchestrator, limits);
    return calls[0]?.timeoutMs;
  };

  assert.equal(await asked(undefined), 15_000);
  assert.equal(await asked({ timeoutMs: 100 }), 100);
  assert.equal(await asked({ timeoutMs: 5_000_000 }), 60_000);
  assert.equal(await asked({ timeoutMs: -5 }), 1);
  assert.equal(await asked({ timeoutMs: Number.NaN }), 15_000);
  assert.equal(await asked({ timeoutMs: "soon" }), 15_000);
});

test("reports failures as data with a code", async () => {
  const timeout = recorder(new OrchestratorError("timeout", "no answer within 100 ms"));
  assert.deepEqual(failure(await handleRequest({ type: "ningo.status" }, timeout.orchestrator, limits)), {
    code: "timeout",
    message: "no answer within 100 ms",
  });

  const unavailable = recorder(new OrchestratorError("unavailable", "nobody is listening"));
  assert.equal(
    failure(await handleRequest({ type: "ningo.status" }, unavailable.orchestrator, limits)).code,
    "unavailable",
  );

  const refused = recorder({ status: "error", error: "switched off" });
  assert.deepEqual(failure(await handleRequest({ type: "ningo.status" }, refused.orchestrator, limits)), {
    code: "orchestrator",
    message: "switched off",
  });

  const silent = recorder({ status: "error" });
  assert.equal(
    failure(await handleRequest({ type: "ningo.status" }, silent.orchestrator, limits)).code,
    "orchestrator",
  );

  const odd = recorder({ status: "ok", result: null });
  assert.equal(
    failure(await handleRequest({ type: "ningo.status" }, odd.orchestrator, limits)).code,
    "unexpected",
  );
});

test("end to end over a real socket, the renderer never sees channel IDs or provider profiles", async () => {
  const server = await startReplyServer(() =>
    ok({
      workflow: "ningo",
      domain: "system",
      result: {
        status: "degraded",
        services: { chat: { status: "error", error: "connection refused" } },
        channels: { meme: ["channel-1"] },
        chat_profiles: { profiles: [{ name: "profile-1", base_url: "base-url-1" }] },
      },
    }),
  );
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint });
    const result = await handleRequest({ type: "ningo.status" }, client, limits);

    assert.deepEqual(result, {
      ok: true,
      result: {
        status: "degraded",
        services: { chat: { status: "error", detail: "connection refused" } },
      },
    });
    assert.equal(server.requests[0]?.type, "ningo.status");
  } finally {
    await server.close();
  }
});

test("isAllowedAction accepts only the exact action names", () => {
  assert.equal(isAllowedAction("ningo.status"), true);
  assert.equal(isAllowedAction("services.set"), true);
  assert.equal(isAllowedAction("scheduler.jobs.set"), true);
  for (const value of ["Ningo.status", "ningo.status ", "services.set ", "scheduler.jobs.announce", 1, null, undefined]) {
    assert.equal(isAllowedAction(value), false);
  }
});

// A change: the shell lets it through only just after real input.
const note = { mode: "note", title: "A thought", body: "Something to keep." };
const ingested = {
  workflow: "knowledge",
  domain: "knowledge",
  result: { source_id: "note:a-thought-1", created: true, changed: true, chunks: 1, embedded: 1, degraded: false },
};
const here = { recent: () => true };
const away = { recent: () => false };

test("a change is refused without a recent click or key press, and the orchestrator never hears of it", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: ingested });
  for (const presence of [away, undefined]) {
    const error = failure(await handleRequest({ type: "knowledge.ingest", payload: note }, orchestrator, limits, undefined, presence));
    assert.equal(error.code, "denied");
    assert.match(error.message, /click or a key press/);
  }
  assert.equal(calls.length, 0);
});

test("a change goes through right after real input, with the payload the shell built", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: ingested });
  const result = await handleRequest(
    { type: "knowledge.ingest", payload: { ...note, kind: "idea", reference: "Book, p. 12", source_id: "other:document", source: "plugin:thing" } },
    orchestrator, limits, undefined, here,
  );

  assert.deepEqual(result, { ok: true, result: { count: 1, created: 1, updated: 0, unchanged: 0, chunks: 1, degraded: false, reason: "" } });
  assert.equal(calls.length, 1);
  const sent = calls[0]?.payload ?? {};
  assert.equal(sent.kind, "idea", "the person chooses the kind");
  assert.equal(sent.source_ref, "Book, p. 12", "and where it came from");
  assert.match(String(sent.source_id), /^note:a-thought-[0-9a-f]{10}$/, "the id the screen named is not the service's field");
  assert.equal(sent.source, undefined);
});

test("reading needs no input: only a change does", async () => {
  const { orchestrator } = recorder({ status: "ok", result: compactStatus });
  const result = await handleRequest({ type: "ningo.status" }, orchestrator, limits, undefined, away);
  assert.equal(result.ok, true);
});

test("a file may weigh what a file weighs, a note not", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: ingested });
  const file = { mode: "file", filename: "reading.txt", content: "QUJD".repeat(2_000_000) };
  const big = await handleRequest({ type: "knowledge.ingest", payload: file }, orchestrator, limits, undefined, here);
  assert.equal(big.ok, true, "a file of about 6 MB passes the outer bound");

  const huge = { ...file, content: "QUJD".repeat(3_100_000) };
  assert.equal(failure(await handleRequest({ type: "knowledge.ingest", payload: huge }, orchestrator, limits, undefined, here)).code, "invalid");
  const longNote = { mode: "note", title: "T", body: "x".repeat(200_001) };
  assert.equal(failure(await handleRequest({ type: "knowledge.ingest", payload: longNote }, orchestrator, limits, undefined, here)).code, "invalid");
  assert.equal(calls.length, 1);

  // the rest of the actions keep the small bound
  const bigRead = { limit: 3, filler: "x".repeat(20_000) };
  assert.equal(failure(await handleRequest({ type: "digest.items", payload: bigRead }, orchestrator, limits, undefined, here)).code, "invalid");
});

test("a switched-off knowledge service is an answer to a change too", async () => {
  const { orchestrator } = recorder({
    status: "ok",
    result: { workflow: "knowledge", domain: "knowledge", result: { skipped: true, reason: "paused by the operator" } },
  });
  const result = await handleRequest({ type: "knowledge.ingest", payload: note }, orchestrator, limits, undefined, here);
  assert.deepEqual(result, { ok: true, result: { off: true, reason: "paused by the operator" } });
});

const documents = {
  workflow: "knowledge",
  domain: "knowledge",
  result: {
    documents: [
      { source_id: "note:a-1", title: "A note", kind: "note", source_ref: "Book, p. 12", tags: ["a", "b"], chunks: 3, embedded: 2, updated_at: "2026-10-02T10:00:00", extra: "x" },
    ],
    total: 41,
    offset: 20,
  },
};

test("the list of documents needs no input, is clamped, and hands back only what a card reads", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: documents });
  const result = await handleRequest(
    { type: "knowledge.list", payload: { limit: 9999, offset: -5, filler: "x" } },
    orchestrator, limits, undefined, away,
  );

  assert.deepEqual(calls[0]?.payload, { limit: 100, offset: 0 });
  assert.deepEqual(result, {
    ok: true,
    result: {
      documents: [{ id: "note:a-1", title: "A note", kind: "note", reference: "Book, p. 12", tags: ["a", "b"], chunks: 3, embedded: 2, updatedAt: "2026-10-02T10:00:00" }],
      total: 41,
      offset: 20,
    },
  });
});

test("indexing the waiting passages is a change, but not one to ask about", async () => {
  const reindexed = { workflow: "knowledge", domain: "knowledge", result: { embedded: 7, remaining: 3, degraded: false } };
  const { orchestrator, calls } = recorder({ status: "ok", result: reindexed });

  assert.equal(failure(await handleRequest({ type: "knowledge.reindex" }, orchestrator, limits, undefined, away)).code, "denied");
  assert.equal(calls.length, 0);

  const asked: string[] = [];
  const result = await handleRequest({ type: "knowledge.reindex", payload: { limit: 99_999, x: 1 } }, orchestrator, limits, undefined, here, async (type) => {
    asked.push(type);
    return true;
  });
  assert.deepEqual(result, { ok: true, result: { embedded: 7, remaining: 3, degraded: false, reason: "" } });
  assert.deepEqual(calls[0]?.payload, { limit: 64 }, "a batch stays small, whatever the screen asks for");
  assert.deepEqual(asked, [], "nothing is lost by indexing, so nobody is asked");

  await handleRequest({ type: "knowledge.reindex" }, orchestrator, limits, undefined, here);
  assert.deepEqual(calls[1]?.payload, { limit: 32 });
});

const deleted = { workflow: "knowledge", domain: "knowledge", result: { source_id: "note:a-1", deleted: true } };

test("a deletion is refused without recent input, before anyone is asked", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: deleted });
  let asked = 0;
  const confirm = async () => {
    asked += 1;
    return true;
  };
  for (const presence of [away, undefined]) {
    const error = failure(await handleRequest({ type: "knowledge.delete", payload: { id: "note:a-1" } }, orchestrator, limits, undefined, presence, confirm));
    assert.equal(error.code, "denied");
  }
  assert.equal(asked, 0, "a script that clicks nothing cannot even open the window");
  assert.equal(calls.length, 0);
});

test("a deletion asks the person, names the id the shell will send, and goes through only on a yes", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: deleted });
  const asked: Array<{ type: string; wire: Record<string, unknown> }> = [];

  const no = await handleRequest({ type: "knowledge.delete", payload: { id: "note:a-1", source_id: "other:doc", title: "a fake title" } }, orchestrator, limits, undefined, here, async (type, wire) => {
    asked.push({ type, wire });
    return false;
  });
  assert.equal(failure(no).code, "cancelled");
  assert.equal(calls.length, 0, "a no never reaches the orchestrator");
  assert.deepEqual(asked, [{ type: "knowledge.delete", wire: { source_id: "note:a-1" } }]);

  const yes = await handleRequest({ type: "knowledge.delete", payload: { id: "note:a-1" } }, orchestrator, limits, undefined, here, async () => true);
  assert.deepEqual(yes, { ok: true, result: { deleted: true } });
  assert.deepEqual(calls.map((call) => [call.type, call.payload]), [["knowledge.delete", { source_id: "note:a-1" }]]);
});

test("a deletion with nobody to ask is refused, and an id outside the rules is invalid before asking", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: deleted });
  const noWay = failure(await handleRequest({ type: "knowledge.delete", payload: { id: "note:a-1" } }, orchestrator, limits, undefined, here));
  assert.equal(noWay.code, "denied");
  assert.match(noWay.message, /confirmation/);

  let asked = 0;
  const confirm = async () => {
    asked += 1;
    return true;
  };
  for (const id of ["", "has space", "a?b", "x".repeat(201), 5, null, {}]) {
    assert.equal(failure(await handleRequest({ type: "knowledge.delete", payload: { id } }, orchestrator, limits, undefined, here, confirm)).code, "invalid", JSON.stringify(id)?.slice(0, 30));
  }
  assert.equal(failure(await handleRequest({ type: "knowledge.delete", payload: {} }, orchestrator, limits, undefined, here, confirm)).code, "invalid");
  assert.equal(asked, 0);
  assert.equal(calls.length, 0);
});

test("a switched-off knowledge service answers a deletion as off", async () => {
  const { orchestrator } = recorder({
    status: "ok",
    result: { workflow: "knowledge", domain: "knowledge", result: { skipped: true, reason: "paused by the operator" } },
  });
  const result = await handleRequest({ type: "knowledge.delete", payload: { id: "note:a-1" } }, orchestrator, limits, undefined, here, async () => true);
  assert.deepEqual(result, { ok: true, result: { off: true, reason: "paused by the operator" } });
});

test("the wait for the person's answer does not count against the request's own time", async () => {
  const server = await startReplyServer(() => ok(deleted));
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint });
    const slowConfirm = async () => {
      await new Promise((resolve) => setTimeout(resolve, 600));
      return true;
    };
    // the request may take 300 ms, the person takes 600 ms: only the request is timed
    const result = await handleRequest(
      { type: "knowledge.delete", payload: { id: "note:a-1" }, options: { timeoutMs: 300 } },
      client, limits, undefined, here, slowConfirm,
    );
    assert.deepEqual(result, { ok: true, result: { deleted: true } });
    assert.equal(server.requests.length, 1);
  } finally {
    await server.close();
  }
});

// The switches: they change something, but only what the operator can switch back.
const switched = { workflow: "ningo", domain: "system", result: { name: "meme", enabled: false } };

test("a switch is refused without a recent click or key press, and the orchestrator never hears of it", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: switched });
  for (const type of ["services.set", "scheduler.jobs.set"]) {
    for (const presence of [away, undefined]) {
      const error = failure(await handleRequest({ type, payload: { name: "meme", enabled: false } }, orchestrator, limits, undefined, presence));
      assert.equal(error.code, "denied", type);
    }
  }
  assert.equal(calls.length, 0);
});

test("a service switch sends only the name and the flag, and asks nobody", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: switched });
  let asked = 0;
  const confirm = async () => { asked += 1; return true; };
  const result = await handleRequest(
    { type: "services.set", payload: { name: "meme", enabled: false, interval: "1m", extra: "x" } },
    orchestrator, limits, undefined, here, confirm,
  );
  assert.deepEqual(result, { ok: true, result: { name: "meme", enabled: false } });
  assert.deepEqual(calls.map((call) => [call.type, call.payload]), [["services.set", { name: "meme", enabled: false }]]);
  assert.equal(asked, 0, "a switch can be switched back, so nobody is asked");
});

test("a service switch with a name or a flag outside the rules is invalid before the orchestrator", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: switched });
  const bad = [
    { enabled: true }, { name: "meme" }, { name: "meme", enabled: "false" }, { name: "meme", enabled: 0 }, { name: "meme", enabled: null },
    { name: "", enabled: true }, { name: "Meme", enabled: true }, { name: "has space", enabled: true }, { name: "a/b", enabled: true },
    { name: "x".repeat(41), enabled: true }, { name: 5, enabled: true },
  ];
  for (const payload of bad) {
    assert.equal(failure(await handleRequest({ type: "services.set", payload }, orchestrator, limits, undefined, here)).code, "invalid", JSON.stringify(payload));
  }
  assert.equal(calls.length, 0);
});

test("switching a job on or off does not touch its interval, and an interval does not touch its switch", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: { workflow: "ningo", domain: "system", result: { name: "meme.refresh", enabled: true, interval_override: true, interval: "6h0m0s" } } });
  const run = (payload: Record<string, unknown>) => handleRequest({ type: "scheduler.jobs.set", payload }, orchestrator, limits, undefined, here);

  assert.deepEqual(await run({ name: "meme.refresh", enabled: false, extra: "x" }), { ok: true, result: { name: "meme.refresh", enabled: true, interval: "6h0m0s", intervalOverride: true } });
  await run({ name: "meme.refresh", interval: "45m" });
  await run({ name: "meme.refresh", interval: "default" });
  await run({ name: "meme.refresh", enabled: true, interval: "6h" });

  assert.deepEqual(calls.map((call) => call.payload), [
    { name: "meme.refresh", enabled: false },
    { name: "meme.refresh", interval: "45m" },
    { name: "meme.refresh", interval: "default" },
    { name: "meme.refresh", enabled: true, interval: "6h" },
  ]);
});

test("a job change that says nothing, or says it wrongly, is invalid: an empty interval would clear the override", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: switched });
  const bad = [
    { name: "meme.refresh" }, { enabled: true }, { name: "meme.refresh", enabled: "yes" },
    { name: "meme.refresh", interval: "" }, { name: "meme.refresh", interval: " " }, { name: "meme.refresh", interval: "soon" },
    { name: "meme.refresh", interval: "0.5h" }, { name: "meme.refresh", interval: "-5m" }, { name: "meme.refresh", interval: "100000m" },
    { name: "meme.refresh", interval: "5m; rm" }, { name: "meme.refresh", interval: 45 }, { name: "meme.refresh", interval: null },
    { name: "Meme Refresh", enabled: true },
  ];
  for (const payload of bad) {
    assert.equal(failure(await handleRequest({ type: "scheduler.jobs.set", payload }, orchestrator, limits, undefined, here)).code, "invalid", JSON.stringify(payload));
  }
  assert.equal(calls.length, 0);
});

test("the scheduler's own heartbeat stays out of reach of the screen", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: switched });
  const error = failure(await handleRequest({ type: "scheduler.jobs.announce", payload: { jobs: [] } }, orchestrator, limits, undefined, here));
  assert.equal(error.code, "denied");
  assert.equal(calls.length, 0);
});

test("the status carries the jobs when the orchestrator reports them, and nothing when it does not", async () => {
  const withJobs = {
    workflow: "ningo",
    domain: "system",
    result: {
      status: "ok",
      services: { meme: { status: "ok", enabled: true }, scheduler: { status: "ok", detail: "rodando", enabled: true } },
      jobs: [
        { name: "meme.refresh", enabled: true, interval: "6h0m0s", interval_override: false, next_at: "2026-10-03T12:00:00Z", last_at: "2026-10-03T06:00:00Z", last_outcome: "ran", last_error: "", announced_at: "x", secret: "y" },
        { name: "meme.dispatch", enabled: false, interval_override: true, last_outcome: "error", last_error: "e".repeat(500) },
        "not a job",
      ],
    },
  };
  const { orchestrator } = recorder({ status: "ok", result: withJobs });
  const result = await handleRequest({ type: "ningo.status" }, orchestrator, limits);
  assert.ok(result.ok);
  const body = result.result as { services: Record<string, unknown>; jobs: Array<Record<string, unknown>> };
  assert.deepEqual(body.services.meme, { status: "ok", detail: "", enabled: true });
  assert.equal(body.jobs.length, 2);
  assert.deepEqual(body.jobs[0], { name: "meme.refresh", enabled: true, interval: "6h0m0s", intervalOverride: false, nextAt: "2026-10-03T12:00:00Z", lastAt: "2026-10-03T06:00:00Z", lastOutcome: "ran", lastError: "" });
  assert.equal(body.jobs[1]?.enabled, false);
  assert.equal(body.jobs[1]?.intervalOverride, true);
  assert.ok(String(body.jobs[1]?.lastError).length <= 200, "a long error is cut");
  assert.doesNotMatch(JSON.stringify(result), /secret|announced_at/);

  const bare = recorder({ status: "ok", result: compactStatus });
  const plain = await handleRequest({ type: "ningo.status" }, bare.orchestrator, limits);
  assert.ok(plain.ok);
  assert.equal("jobs" in (plain.result as object), false);
});

test("every interval the card's editor can offer goes through the shell, up to the longest the orchestrator keeps", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: { workflow: "ningo", domain: "system", result: { name: "meme.refresh", enabled: true, interval_override: true, interval: "168h0m0s" } } });
  const intervals = ["1m", "45m", "10080m", "43200m", "1h", "168h", "720h"];
  for (const interval of intervals) {
    const result = await handleRequest({ type: "scheduler.jobs.set", payload: { name: "meme.refresh", interval } }, orchestrator, limits, undefined, here);
    assert.ok(result.ok, interval);
  }
  assert.deepEqual(calls.map((call) => call.payload.interval), intervals);
});

// ---- linking an interface ----

const linked = {
  workflow: "interface-link",
  domain: "system",
  result: { interface: "whatsapp", state: "connecting" },
};
const linkPayload = { interface: "whatsapp" };

test("reading the link state needs no click, and sends the fixed interface", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: linked });

  const result = await handleRequest({ type: "communications.link.status", payload: { ...linkPayload, extra: 1 } }, orchestrator, limits, undefined, away);

  assert.deepEqual(result, { ok: true, result: { state: "connecting" } });
  assert.deepEqual(calls.map((call) => [call.type, call.payload]), [["communications.link.status", { interface: "whatsapp" }]]);
});

test("asking for a new code is a change: refused without recent input, never asked in a window", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: linked });
  let asked = 0;
  const confirm = async () => {
    asked += 1;
    return true;
  };

  for (const presence of [away, undefined]) {
    const error = failure(await handleRequest({ type: "communications.link.start", payload: linkPayload }, orchestrator, limits, undefined, presence, confirm));
    assert.equal(error.code, "denied");
  }
  assert.equal(calls.length, 0);

  const result = await handleRequest({ type: "communications.link.start", payload: linkPayload }, orchestrator, limits, undefined, here, confirm);
  assert.equal(result.ok, true);
  assert.equal(asked, 0, "a new code asks for no window");
});

test("starting over waits for the person's yes, asked by the shell, and a no never reaches the orchestrator", async () => {
  const { orchestrator, calls } = recorder({ status: "ok", result: linked });
  const asked: Array<{ type: string; wire: Record<string, unknown> }> = [];

  for (const presence of [away, undefined]) {
    const error = failure(await handleRequest({ type: "communications.link.reset", payload: linkPayload }, orchestrator, limits, undefined, presence, async () => true));
    assert.equal(error.code, "denied");
  }

  const no = await handleRequest({ type: "communications.link.reset", payload: linkPayload }, orchestrator, limits, undefined, here, async (type, wire) => {
    asked.push({ type, wire });
    return false;
  });
  assert.equal(failure(no).code, "cancelled");
  assert.equal(calls.length, 0);
  assert.deepEqual(asked, [{ type: "communications.link.reset", wire: { interface: "whatsapp" } }]);

  const yes = await handleRequest({ type: "communications.link.reset", payload: linkPayload }, orchestrator, limits, undefined, here, async () => true);
  assert.equal(yes.ok, true);
  assert.deepEqual(calls.map((call) => call.type), ["communications.link.reset"]);

  const noWindow = failure(await handleRequest({ type: "communications.link.reset", payload: linkPayload }, orchestrator, limits, undefined, here));
  assert.equal(noWindow.code, "denied", "with no way to ask, starting over is refused");
});

test("the link actions are on the allow-list and no neighbour is", () => {
  for (const type of ["communications.link.status", "communications.link.start", "communications.link.reset"]) {
    assert.equal(isAllowedAction(type), true, type);
  }
  for (const type of ["communications.link", "communications.link.stop", "communications.link.status ", "link.reset"]) {
    assert.equal(isAllowedAction(type), false, type);
  }
});


test("Knowledge's inner write timeout is an unknown outcome, never a definite ingestion failure", async () => {
  const { orchestrator, calls } = recorder({ status: "error", error: "dispatch workflow: dispatch knowledge: knowledge_write_outcome_unknown: deadline passed" });
  const answer = await handleRequest({ type: "knowledge.ingest", payload: { mode: "note", title: "Fixture", body: "fixture text" } }, orchestrator, limits, undefined, { recent: () => true });
  assert.equal(failure(answer).code, "timeout");
  assert.match(failure(answer).message, /unknown/);
  assert.equal(calls.length, 1, "a timed-out write must not be retried automatically");
});


test("an archived message forwards only its conversation, send id and text, or a bare retry", async () => {
  const conversation = "a".repeat(32);
  const turnId = "b".repeat(32);
  const calls: unknown[] = [];
  const turn = {id:turnId,seq:1,send_id:"send-0001",content:"explain",reply:"Think through the evidence.",state:"completed",reason:"",attempts:1};
  const client = { async request(type: string, payload: Record<string, unknown>) { calls.push({type,payload}); return {status:"ok",result:{domain:"chat",result:{reply:"Think through the evidence.",turn}}}; } };
  const limits = {defaultTimeoutMs:15000,maxTimeoutMs:120000};
  const present = {recent:()=>true};
  const message = {content:"explain",conversation_id:conversation,send_id:"send-0001"};
  assert.equal((await handleRequest({type:"message.created",payload:message},client,limits)).ok,false, "a message needs a recent gesture");
  const answer = await handleRequest({type:"message.created",payload:{...message,reading_id:"entry-1",system:"inject"}},client,limits,undefined,present);
  assert.deepEqual(answer,{ok:true,result:{reply:"Think through the evidence.",replayed:false,turn:{id:turnId,seq:1,sendId:"send-0001",content:"explain",reply:"Think through the evidence.",state:"completed",reason:"",attempts:1}}});
  await handleRequest({type:"message.created",payload:{conversation_id:conversation,retry_turn_id:turnId,content:"ignored",send_id:"send-0002"}},client,limits,undefined,present);
  assert.deepEqual(calls,[
    {type:"message.created",payload:message},
    {type:"message.created",payload:{conversation_id:conversation,retry_turn_id:turnId}},
  ]);
  for (const payload of [
    {content:"explain",conversation_id:"thread-1",send_id:"send-0001"},
    {content:"explain",conversation_id:conversation},
    {content:"explain",conversation_id:conversation,send_id:"short"},
    {content:"",conversation_id:conversation,send_id:"send-0001"},
    {conversation_id:conversation,retry_turn_id:"not-a-turn"},
  ]) {
    const refused = await handleRequest({type:"message.created",payload},client,limits,undefined,present);
    assert.equal(refused.ok,false,JSON.stringify(payload));
  }
  assert.equal(calls.length,2,"a refused message never reaches the orchestrator");
});

test("a conversation is started with only its reading, and read by its archive id", async () => {
  const conversation = {id:"c".repeat(32),kind:"reading",reading_id:"entry-1",title:"",updated_at:"t",revision:1,turns:0};
  const calls: unknown[] = [];
  const client = { async request(type: string, payload: Record<string, unknown>) {
    calls.push({type,payload});
    return {status:"ok",result:{domain:"conversation",result:type==="conversation.read"?{conversation,turns:[{id:"d".repeat(32),seq:1,send_id:"send-0001",content:"q",reply:"",state:"bogus"}]}:{conversation}}};
  } };
  const limits = {defaultTimeoutMs:15000,maxTimeoutMs:120000};
  assert.equal((await handleRequest({type:"conversation.start",payload:{reading_id:"entry-1"}},client,limits)).ok,false, "starting one records it, so it needs a gesture");
  const started = await handleRequest({type:"conversation.start",payload:{reading_id:"entry-1",title:"  First   line ",owner:"x"}},client,limits,undefined,{recent:()=>true});
  assert.deepEqual(started,{ok:true,result:{conversation:{id:"c".repeat(32),kind:"reading",readingId:"entry-1",title:"",updatedAt:"t",revision:1,turns:0}}});
  const read = await handleRequest({type:"conversation.read",payload:{conversation_id:"c".repeat(32),limit:5000}},client,limits);
  assert.equal(read.ok,true);
  assert.deepEqual((read as {result:{turns:unknown[]}}).result.turns,[],"a turn in an unknown state is dropped");
  assert.equal((await handleRequest({type:"conversation.read",payload:{conversation_id:"../x"}},client,limits)).ok,false);
  assert.equal((await handleRequest({type:"conversation.start",payload:{title:"t".repeat(121)}},client,limits,undefined,{recent:()=>true})).ok,false,"a title past the archive's bound is refused");
  assert.deepEqual(calls,[
    {type:"conversation.start",payload:{reading_id:"entry-1",title:"First line"}},
    {type:"conversation.read",payload:{conversation_id:"c".repeat(32),limit:100}},
  ]);
});

test("the conversation list is a bounded read, with only the archive's own cursor", async () => {
  const calls: unknown[] = [];
  const item = (n: string) => ({id:n.repeat(32),kind:"general",reading_id:"",title:"t",updated_at:"u",revision:1,turns:2});
  const client = { async request(type: string, payload: Record<string, unknown>) {
    calls.push({type,payload});
    return {status:"ok",result:{domain:"conversation",result:{conversations:[item("a"),item("b")],next:"12.".concat("f".repeat(32))}}};
  } };
  const limits = {defaultTimeoutMs:15000,maxTimeoutMs:120000};
  const listed = await handleRequest({type:"conversation.list",payload:{limit:20}},client,limits);
  assert.equal(listed.ok,true);
  assert.equal((listed as {result:{conversations:unknown[];next:string}}).result.conversations.length,2);
  assert.equal((listed as {result:{next:string}}).result.next,"12.".concat("f".repeat(32)));
  await handleRequest({type:"conversation.list",payload:{cursor:"12.".concat("f".repeat(32)),scope:"other"}},client,limits);
  for (const payload of [{limit:51},{limit:0},{cursor:"anything"},{limit:2.5}]) {
    assert.equal((await handleRequest({type:"conversation.list",payload},client,limits)).ok,false,JSON.stringify(payload));
  }
  assert.deepEqual(calls,[
    {type:"conversation.list",payload:{limit:20}},
    {type:"conversation.list",payload:{cursor:"12.".concat("f".repeat(32)),limit:50}},
  ]);
});


test("reading media enters the preview gate without authorizing its article address", async () => {
  const remembered: string[][] = [];
  const { orchestrator } = recorder({ status: "ok", result: { domain: "digest", result: { items: [
    { id: "picture", title: "Collected picture", media_url: "https://images.example/reading.jpg", url: "https://example.com/article" },
    { id: "article", title: "Article", url: "https://example.com/other" },
  ] } } });
  const result = await handleRequest({ type: "digest.items" }, orchestrator, limits, { remember: urls => remembered.push(urls) });
  assert.equal(result.ok, true);
  assert.deepEqual(remembered, [["https://images.example/reading.jpg"]]);
});

test("renaming needs a press and deleting also the person's yes; both name the version they saw", async () => {
  const id = "c".repeat(32);
  const calls: unknown[] = [];
  const conversation = {id,kind:"general",reading_id:"",title:"New",updated_at:"u",revision:2,turns:1};
  const client = { async request(type: string, payload: Record<string, unknown>) {
    calls.push({type,payload});
    return {status:"ok",result:{domain:"conversation",result:type==="conversation.delete"?{conversation_id:id,deleted:true}:{conversation}}};
  } };
  const limits = {defaultTimeoutMs:15000,maxTimeoutMs:120000};
  const present = {recent:()=>true};
  const rename = {conversation_id:id,revision:1,title:"  New   name "};
  assert.equal((await handleRequest({type:"conversation.rename",payload:rename},client,limits)).ok,false,"a rename needs a press");
  const renamed = await handleRequest({type:"conversation.rename",payload:{...rename,scope:"other"}},client,limits,undefined,present);
  assert.equal(renamed.ok,true);
  for (const payload of [{conversation_id:id,title:"x"},{conversation_id:id,revision:0,title:"x"},{conversation_id:id,revision:1.5,title:"x"},{conversation_id:"bad",revision:1,title:"x"},{conversation_id:id,revision:1,title:"t".repeat(121)}]) {
    assert.equal((await handleRequest({type:"conversation.rename",payload},client,limits,undefined,present)).ok,false,JSON.stringify(payload));
  }
  const asked: unknown[] = [];
  const no = async (type: string, wire: Record<string, unknown>) => { asked.push({type,wire}); return false; };
  const yes = async () => true;
  const refused = await handleRequest({type:"conversation.delete",payload:{conversation_id:id,revision:2}},client,limits,undefined,present,no);
  assert.equal(refused.ok,false);
  assert.deepEqual(asked,[{type:"conversation.delete",wire:{conversation_id:id,revision:2}}]);
  const deleted = await handleRequest({type:"conversation.delete",payload:{conversation_id:id,revision:2}},client,limits,undefined,present,yes);
  assert.deepEqual(deleted,{ok:true,result:{deleted:true}});
  assert.deepEqual(calls,[
    {type:"conversation.rename",payload:{conversation_id:id,revision:1,title:"New name"}},
    {type:"conversation.delete",payload:{conversation_id:id,revision:2}},
  ]);
});

test("listing WhatsApp groups is a read of the fixed interface, and only group ids reach the screen", async () => {
  const calls: unknown[] = [];
  const client = { async request(type: string, payload: Record<string, unknown>) {
    calls.push({type,payload});
    return {status:"ok",result:{domain:"system",result:{interface:"whatsapp",groups:[
      {id:"120363000000000001@g.us",name:"ningo",members:1},
      {id:"5511900000000@s.whatsapp.net",name:"a person",members:2},
      {id:"120363000000000002@g.us",name:"x".repeat(300),members:"many"},
    ]}}};
  } };
  const limits = {defaultTimeoutMs:15000,maxTimeoutMs:120000};
  const answer = await handleRequest({type:"communications.link.groups",payload:{interface:"whatsapp",extra:"x"}},client,limits);
  assert.equal(answer.ok,true);
  const groups = (answer as {result:{groups:{id:string;name:string;members:number}[]}}).result.groups;
  assert.deepEqual(groups.map((group) => group.id),["120363000000000001@g.us","120363000000000002@g.us"]);
  assert.equal(groups[1]!.name.length,100);
  assert.equal(groups[1]!.members,0);
  assert.equal((await handleRequest({type:"communications.link.groups",payload:{interface:"discord"}},client,limits)).ok,false);
  assert.deepEqual(calls,[{type:"communications.link.groups",payload:{interface:"whatsapp"}}]);
});
