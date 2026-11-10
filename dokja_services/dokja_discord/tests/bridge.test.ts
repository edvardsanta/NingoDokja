import test from "node:test";
import assert from "node:assert/strict";

import { buildBridgePayload, dispatchToOrchestrator, dispatchToOrchestratorResponse, FETCH_HEADERS_LIMIT_MS } from "../transport/bridge.js";

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



    assert.equal(reply, "hello back");
  } finally {
    globalThis.fetch = originalFetch;
  }
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

// Regression: Node's fetch drops a request after five minutes without headers, which failed every
// quiz run that took longer. A request allowed to wait past that goes through node:http instead.
test("only a request allowed past fetch's headers limit leaves fetch for node:http", async () => {
  const { createServer } = await import("node:http");
  const server = createServer((request, response) => {
    request.resume();
    request.on("end", () => {
      setTimeout(() => {
        response.writeHead(200, { "content-type": "application/json" });
        response.end(JSON.stringify({ status: "ok", reply: "slow but fine" }));
      }, 50);
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as { port: number };
  const endpoint = `http://127.0.0.1:${port}/orchestrator`;
  const originalFetch = globalThis.fetch;
  let fetched = 0;
  globalThis.fetch = (async (...args: Parameters<typeof fetch>) => { fetched++; return originalFetch(...args); }) as typeof fetch;
  try {
    const body = buildBridgePayload({ eventType: "quiz.take", content: "https://quiz.example.com", userId: "u", channelId: "c", messageId: "m" });
    const long = await dispatchToOrchestratorResponse(endpoint, FETCH_HEADERS_LIMIT_MS, body);
    assert.equal(long.reply, "slow but fine");
    assert.equal(fetched, 0);
    const short = await dispatchToOrchestratorResponse(endpoint, 30_000, body);
    assert.equal(short.reply, "slow but fine");
    assert.equal(fetched, 1);
  } finally {
    globalThis.fetch = originalFetch;
    await new Promise((resolve) => server.close(resolve));
  }
});
