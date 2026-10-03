import test from "node:test";
import assert from "node:assert/strict";

import { OrchestratorClient, OrchestratorError } from "../src/main/orchestrator_client.js";
import { ok, sleep, startReplyServer } from "./support/reply_server.js";

function isCode(code: string) {
  return (error: unknown): boolean => {
    assert.ok(error instanceof OrchestratorError, "expected an OrchestratorError");
    assert.equal(error.code, code);
    return true;
  };
}

test("sends the envelope the orchestrator expects and returns its reply", async () => {
  const server = await startReplyServer(() => ok({ answered: true }));
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint });
    const reply = await client.request("ningo.status", { limit: 2 }, 2_000);

    assert.equal(reply.status, "ok");
    assert.deepEqual(reply.result, { answered: true });

    const [event] = server.requests;
    assert.ok(event);
    assert.equal(event.type, "ningo.status");
    assert.equal(event.source, "desktop");
    assert.deepEqual(event.payload, { limit: 2 });
    assert.match(
      String(event.event_id),
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/,
    );
    assert.ok(Number.isFinite(Date.parse(String(event.timestamp))));
    assert.deepEqual(event.user, { id: "desktop", name: "desktop" });
    assert.deepEqual(event.channel, { id: "desktop" });
    assert.deepEqual(event.context, { interface: "desktop" });
  } finally {
    await server.close();
  }
});

test("gives up at the deadline and the next request gets its own reply, not the stale one", async () => {
  // The first request stalls past its deadline; the server only answers it later.
  const server = await startReplyServer(async (request, index) => {
    if (index === 1) await sleep(300);
    return ok({ answered: request.payload });
  });
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint });
    const started = Date.now();
    await assert.rejects(client.request("ningo.status", { n: 1 }, 100), isCode("timeout"));
    assert.ok(Date.now() - started < 280, "the deadline was honoured");

    const reply = await client.request("ningo.status", { n: 2 }, 3_000);
    assert.deepEqual(reply.result, { answered: { n: 2 } });
    assert.deepEqual(
      server.requests.map((request) => request.payload),
      [{ n: 1 }, { n: 2 }],
    );
  } finally {
    await server.close();
  }
});

test("counts the wait in line against the deadline and never sends an expired request", async () => {
  const server = await startReplyServer(async (request, index) => {
    if (index === 1) await sleep(400);
    return ok({ answered: request.payload });
  });
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint });
    const slow = client.request("ningo.status", { n: "slow" }, 5_000);
    const impatient = client.request("ningo.status", { n: "impatient" }, 100);

    const started = Date.now();
    await assert.rejects(impatient, isCode("timeout"));
    assert.ok(Date.now() - started < 300, "gave up while the first request was still running");

    assert.deepEqual((await slow).result, { answered: { n: "slow" } });
    await sleep(100); // let the line reach the expired request
    assert.deepEqual(
      server.requests.map((request) => request.payload),
      [{ n: "slow" }],
    );
  } finally {
    await server.close();
  }
});

test("reports the orchestrator as unavailable quickly when nothing is listening", async () => {
  const server = await startReplyServer(() => ok({}));
  const endpoint = server.endpoint;
  await server.close(); // the port is free again and nobody listens

  const client = new OrchestratorClient({ endpoint, connectTimeoutMs: 150 });
  const started = Date.now();
  await assert.rejects(client.request("ningo.status", {}, 5_000), isCode("unavailable"));
  assert.ok(Date.now() - started < 1_000, "failed fast instead of waiting for the deadline");
});

test("rejects a reply that is not a JSON object with a status", async () => {
  const replies = ["not json", "[]", JSON.stringify({ result: 1 })];
  const server = await startReplyServer((_request, index) => replies[index - 1] ?? "");
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint });
    for (const _reply of replies) {
      await assert.rejects(client.request("ningo.status", {}, 2_000), isCode("orchestrator"));
    }
  } finally {
    await server.close();
  }
});

test("sends requests one at a time in the order they were made", async () => {
  const server = await startReplyServer(async (request, index) => {
    await sleep(index === 1 ? 60 : 0);
    return ok({ answered: request.payload });
  });
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint });
    const replies = await Promise.all(
      [1, 2, 3].map((n) => client.request("ningo.status", { n }, 5_000)),
    );

    assert.deepEqual(
      replies.map((reply) => reply.result),
      [1, 2, 3].map((n) => ({ answered: { n } })),
    );
    assert.deepEqual(
      server.requests.map((request) => request.payload),
      [{ n: 1 }, { n: 2 }, { n: 3 }],
    );
  } finally {
    await server.close();
  }
});
