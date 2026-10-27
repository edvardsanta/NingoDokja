import test from "node:test";
import assert from "node:assert/strict";

import { buildBridgePayload, dispatchToOrchestrator, dispatchToOrchestratorResponse } from "../transport/bridge.js";

test("bridge forwards bounded formula PNGs and drops unsafe attachments", async () => {
  const originalFetch = globalThis.fetch;
  const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6JXAAAAAASUVORK5CYII=";
  const valid = { filename: "formula-1.png", content_type: "image/png", data_b64: png };
  const music = { guild_id: "guild-fixture", queue: [] };
  const body = buildBridgePayload({ eventType: "message.created", userId: "u", channelId: "c", messageId: "m" });
  globalThis.fetch = (async () => new Response(JSON.stringify({
    status: "ok", reply: "x / 2", music, attachments: [valid, { ...valid, filename: "../../anything" }, { ...valid, content_type: "text/html" }],
  }))) as typeof fetch;
  try {
    const result = await dispatchToOrchestratorResponse("http://orch", 1000, body);
    assert.equal(result.reply, "x / 2");
    assert.deepEqual(result.attachments, [valid]);
    assert.deepEqual(result.music, music);
  } finally { globalThis.fetch = originalFetch; }
});

test("buildBridgePayload maps discord fields into orchestrator bridge request", () => {
  const payload = buildBridgePayload({
    eventType: "meme.fetch",
    limit: 3,
    userId: "u1",
    userName: "Member".repeat(40),
    channelId: "c1",
    messageId: "m1",
    guildId: "g1",
  });

  assert.equal(payload.event_type, "meme.fetch");
  assert.equal(payload.payload.limit, 3);
  assert.equal(payload.payload.userName, "Member".repeat(40).slice(0, 100));
  assert.equal(payload.payload.userId, "u1");
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

    test("dispatchToOrchestratorResponse returns presentation notice metadata", async () => {
      const originalFetch = globalThis.fetch;
      globalThis.fetch = (async () =>
        new Response(JSON.stringify({ status: "ok", reply: "hello back", notice: "chat.context_compacted" }), {
          status: 200,
          headers: { "content-type": "application/json" },
        })) as typeof fetch;

      try {
        const response = await dispatchToOrchestratorResponse("http://orch", 1000, {
          event_type: "message.created",
          payload: {
            content: "oi",
            userId: "u1",
            channelId: "c1",
            messageId: "m1",
            sessionKey: "discord:c1:u1",
            accountId: "default",
          },
        });
        assert.deepEqual(response, { reply: "hello back", notice: "chat.context_compacted" });
      } finally {
        globalThis.fetch = originalFetch;
      }
    });

    assert.equal(reply, "hello back");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
