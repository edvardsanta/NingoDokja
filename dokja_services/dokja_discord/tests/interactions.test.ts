import test from "node:test";
import assert from "node:assert/strict";

import { buildNingoPanelReply, createInteractionDefinitions } from "../commands/interactions.js";

test("buildNingoPanelReply returns buttons and a select menu", () => {
  const reply = buildNingoPanelReply();
  assert.equal(reply.content, "Ningo controls");
  assert.equal(reply.components.length, 5);
  assert.equal(reply.components[0]?.kind, "button");
  assert.equal(reply.components[4]?.kind, "select");
});

test("chat modal action forwards modal prompt to orchestrator", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (_input, init) => {
    const body = JSON.parse(String(init?.body ?? "{}"));
    assert.equal(body.event_type, "message.created");
    assert.equal(body.payload.content, "hello from modal");
    return new Response(JSON.stringify({ status: "ok", reply: "modal reply" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;

  try {
    const interactions = createInteractionDefinitions({
      orchestratorEndpoint: "http://orch",
      orchestratorTimeoutMs: 1000,
    });
    const modal = interactions.modals.find((entry) => entry.customId === "chat_prompt_modal");
    assert.ok(modal);

    let replyText = "";
    await modal.handle({
      customId: "chat_prompt_modal",
      userId: "u1",
      channelId: "c1",
      messageId: "m1",
      guildId: "g1",
      getText: () => "hello from modal",
      reply: async (message) => {
        replyText = typeof message === "string" ? message : message.content;
      },
    });

    assert.equal(replyText, "modal reply");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
