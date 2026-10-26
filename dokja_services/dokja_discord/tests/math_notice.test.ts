import assert from "node:assert/strict";
import test from "node:test";

import { createCommands } from "../commands/commands.js";
import { createInteractionDefinitions } from "../commands/interactions.js";
import { createDiscordMessageHandler } from "../messages/message_listener.js";
import { localizeNotice } from "../messages/notices.js";
import type { DiscordReply } from "../types.js";

test("formula failures remain visible in messages, slash commands and modals", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async () => new Response(JSON.stringify({
    status: "ok", reply: "x / 2", notice: "chat.math_render_failed",
  }))) as typeof fetch;
  try {
    const replies: string[] = [];
    const context = {
      userId: "u1", channelId: "c1", messageId: "m1", guildId: "g1",
      content: "render a formula", isBot: false, isDirectMessage: false, mentionsBot: true,
      customId: "chat_prompt_modal", getText: () => "render a formula", getString: () => "render a formula",
      getNumber: () => undefined, updateReply: async () => {}, showModal: async () => {},
      reply: async (message: DiscordReply) => { replies.push(typeof message === "string" ? message : message.content); },
    };
    await createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, locale: "en" })(context);
    const command = createCommands("http://orch", 1000, undefined, { locale: "en" }).find(entry => entry.name === "chat");
    assert.ok(command);
    await command.handle(context);
    const modal = createInteractionDefinitions({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, locale: "en" })
      .modals.find(entry => entry.customId === "chat_prompt_modal");
    assert.ok(modal);
    await modal.handle(context);
    assert.equal(replies.length, 3);
    for (const reply of replies) {
      assert.equal(reply, "Some formula images could not be generated. The formulas are included as text.\n\nx / 2");
    }
  } finally { globalThis.fetch = originalFetch; }
});

test("formula failure notices use the interface locale and retain text", () => {
  assert.equal(localizeNotice("chat.math_render_failed", "x / 2"),
    "Algumas fórmulas não puderam ser geradas como imagem. As versões em texto estão abaixo.\n\nx / 2");
  assert.equal(localizeNotice("unknown", "x / 2"), "x / 2");
});
