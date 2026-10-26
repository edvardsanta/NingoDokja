import test from "node:test";
import assert from "node:assert/strict";

import { createDiscordMessageHandler } from "../messages/message_listener.js";

test("message handler delivers formula attachments with readable text", async () => {
  const originalFetch = globalThis.fetch;
  const attachment = { filename: "formula-1.png", content_type: "image/png", data_b64: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6JXAAAAAASUVORK5CYII=" };
  globalThis.fetch = (async () => new Response(JSON.stringify({ status: "ok", reply: "x / 2", attachments: [attachment] }))) as typeof fetch;
  try {
    let delivered: unknown;
    const handler = createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000 });
    await handler({ userId: "u", channelId: "c", messageId: "m", content: "show a formula", isBot: false,
      isDirectMessage: true, mentionsBot: false, reply: async message => { delivered = message; } });
    assert.deepEqual(delivered, { content: "x / 2", attachments: [attachment] });
  } finally { globalThis.fetch = originalFetch; }
});

test("message handler forwards mention facts for domain authorization", async () => {
  const originalFetch = globalThis.fetch;
  let metadata: Record<string, unknown> = {};
  globalThis.fetch = (async (_input, init) => {
    metadata = JSON.parse(String(init?.body ?? "{}")).payload;
    return new Response(JSON.stringify({ status: "ok", reply: "" }), { status: 200 });
  }) as typeof fetch;
  try {
  const handler = createDiscordMessageHandler({
    orchestratorEndpoint: "http://orch",
    orchestratorTimeoutMs: 1000,
  });

  let replied = false;
  await handler({
    userId: "u1",
    channelId: "channel-1",
    messageId: "m1",
    guildId: "guild-1",
    content: "hello there",
    isBot: false,
    isDirectMessage: false,
    mentionsBot: false,
    reply: async () => {
      replied = true;
    },
  });

  assert.equal(replied, false);
  assert.equal(metadata.entryKind, "message");
  assert.equal(metadata.isDirectMessage, false);
  assert.equal(metadata.mentionsBot, false);
  } finally { globalThis.fetch = originalFetch; }
});

test("message handler forwards allowed message and replies with orchestrator output", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (_input, init) => {
    const body = JSON.parse(String(init?.body ?? "{}"));
    assert.equal(body.event_type, "message.created");
    assert.equal(body.payload.content, "hello bot");
    assert.equal(body.payload.replyToContent, "A calculator like Windows?");

    return new Response(JSON.stringify({ status: "ok", reply: "hello human", notice: "chat.context_compacted" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;

  try {
    const handler = createDiscordMessageHandler({
      orchestratorEndpoint: "http://orch",
      orchestratorTimeoutMs: 1000,
    });

    let replyText = "";
    await handler({
      userId: "u1",
      channelId: "channel-1",
      messageId: "m1",
      guildId: "guild-1",
      content: "hello bot",
      replyToContent: "A calculator like Windows?",
      isBot: false,
      isDirectMessage: false,
      mentionsBot: true,
      reply: async (message: string) => {
        replyText = message;
      },
    });

    assert.equal(replyText, "O contexto da conversa foi compactado.\n\nhello human");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
