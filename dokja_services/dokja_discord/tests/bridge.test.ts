import test from "node:test";
import assert from "node:assert/strict";

import { buildBridgePayload, dispatchToOrchestrator } from "../transport/bridge.js";

test("buildBridgePayload maps discord fields into orchestrator bridge request", () => {
  const payload = buildBridgePayload({
    eventType: "meme.fetch",
    limit: 3,
    userId: "u1",
    channelId: "c1",
    messageId: "m1",
    guildId: "g1",
  });

  assert.equal(payload.event_type, "meme.fetch");
  assert.equal(payload.payload.limit, 3);
  assert.equal(payload.payload.sessionKey, "discord:c1:u1");
  assert.equal(payload.payload.accountId, "default");
});

test("dispatchToOrchestrator returns reply payload", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ status: "ok", reply: "hello back" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    })) as typeof fetch;

  try {
    const reply = await dispatchToOrchestrator("http://orch", 1000, {
      event_type: "message.created",
      payload: {
        content: "hello",
        userId: "u1",
        channelId: "c1",
        messageId: "m1",
        sessionKey: "discord:c1:u1",
        accountId: "default",
      },
    });

    assert.equal(reply, "hello back");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
