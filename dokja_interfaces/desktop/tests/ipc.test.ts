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
    "knowledge.add",
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
  assert.deepEqual(calls[0]?.payload, { limit: 3 });
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
