import test from "node:test";
import assert from "node:assert/strict";

import { createDiscordMessageHandler } from "../messages/message_listener.js";

test("message handler ignores non-mentioned guild messages outside DM when bot id is configured", async () => {
  const handler = createDiscordMessageHandler({
    botUserId: "bot-1",
    orchestratorEndpoint: "http://orch",
    orchestratorTimeoutMs: 1000,
    allowedGuildId: "guild-1",
    allowedChannelIds: ["channel-1"],
    dmPolicy: "open",
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
});

test("message handler forwards allowed message and replies with orchestrator output", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (_input, init) => {
    const body = JSON.parse(String(init?.body ?? "{}"));
    assert.equal(body.event_type, "message.created");
    assert.equal(body.payload.content, "hello bot");

    return new Response(JSON.stringify({ status: "ok", reply: "hello human" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;

  try {
    const handler = createDiscordMessageHandler({
      botUserId: "bot-1",
      orchestratorEndpoint: "http://orch",
      orchestratorTimeoutMs: 1000,
      allowedGuildId: "guild-1",
      allowedChannelIds: ["channel-1"],
      dmPolicy: "open",
    });

    let replyText = "";
    await handler({
      userId: "u1",
      channelId: "channel-1",
      messageId: "m1",
      guildId: "guild-1",
      content: "hello bot",
      isBot: false,
      isDirectMessage: false,
      mentionsBot: true,
      reply: async (message: string) => {
        replyText = message;
      },
    });

    assert.equal(replyText, "hello human");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
