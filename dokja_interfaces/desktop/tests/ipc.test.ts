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
    "services.set",
    "scheduler.jobs.set",
    "scheduler.jobs.announce",
    "discord.send",
    "meme.dispatch.scheduled",
    "chat.profile.use",
    "memory.forget",
    "memory.record",
    "memory.resolve",
    "knowledge.add",
    "knowledge.delete",
    "knowledge.reindex",
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
        services: { chat_ai: { status: "error", error: "connection refused" } },
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
        services: { chat_ai: { status: "error", detail: "connection refused" } },
      },
    });
    assert.equal(server.requests[0]?.type, "ningo.status");
  } finally {
    await server.close();
  }
});

test("isAllowedAction accepts only the exact action names", () => {
  assert.equal(isAllowedAction("ningo.status"), true);
  for (const value of ["Ningo.status", "ningo.status ", "services.set", 1, null, undefined]) {
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
    { type: "knowledge.ingest", payload: { ...note, kind: "secret", source_id: "other:document", source: "plugin:thing" } },
    orchestrator, limits, undefined, here,
  );

  assert.deepEqual(result, { ok: true, result: { count: 1, created: 1, updated: 0, unchanged: 0, chunks: 1, degraded: false, reason: "" } });
  assert.equal(calls.length, 1);
  const sent = calls[0]?.payload ?? {};
  assert.equal(sent.kind, "note", "the screen cannot choose the kind");
  assert.match(String(sent.source_id), /^note:a-thought-[0-9a-f]{10}$/, "nor the id");
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
