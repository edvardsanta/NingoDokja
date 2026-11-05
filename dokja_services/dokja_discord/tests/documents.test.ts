import test from "node:test";
import assert from "node:assert/strict";
import { pdfAttachments } from "../messages/documents.js";
import { createDiscordMessageHandler } from "../messages/message_listener.js";
import { toMessageContext } from "../runtime/discordjs_runtime.js";

test("both Discord attachment shapes project PDF metadata without downloading bytes", () => {
  const common = { url: "https://example.com/paper.pdf", size: 1600000 };
  const expected = { ...common, filename: "paper.pdf", content_type: "application/pdf" };
  assert.deepEqual(pdfAttachments([{ ...common, name: "paper.pdf", contentType: "application/pdf" }]), [expected]);
  assert.deepEqual(pdfAttachments([expected, { ...common, filename: "photo.png", content_type: "image/png" }]), [expected]);
  assert.deepEqual(pdfAttachments([null, { ...common, filename: "paper.pdf", size: "bad" }]), []);
});

test("Discord runtime retains attachment-only messages", async () => {
  const attachment = { url: "https://example.com/paper.pdf", name: "paper.pdf", contentType: "application/pdf", size: 1600000 };
  const context = await toMessageContext({
    author: { id: "member", bot: false }, id: "message", channelId: "channel", guildId: "guild",
    content: "", channel: { type: 0 }, mentions: { users: new Map() }, attachments: new Map([["file", attachment]]),
  } as any);
  assert.equal(context.content, "");
  assert.equal(context.documentAttachments?.[0].filename, "paper.pdf");
});

test("an attachment-only message sends metadata to the orchestrator, not a direct reader", async () => {
  const originalFetch = globalThis.fetch;
  const attachment = { url: "https://example.com/paper.pdf", filename: "paper.pdf", content_type: "application/pdf", size: 1600000 };
  let calls = 0;
  globalThis.fetch = (async (input, init) => {
    calls++;
    assert.equal(input, "http://orch");
    const request = JSON.parse(String(init?.body));
    assert.equal(request.payload.content, "");
    assert.equal(request.payload.locale, "en");
    assert.deepEqual(request.payload.documentAttachments, [attachment]);
    return new Response(JSON.stringify({ status: "ok", reply: "Read page 9. [1]" }));
  }) as typeof fetch;
  try {
    let reply: unknown;
    await createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, locale: "en" })({
      userId: "member", channelId: "channel", messageId: "message", content: "", documentAttachments: [attachment],
      isBot: false, isDirectMessage: false, mentionsBot: false, reply: async value => { reply = value; },
    });
    assert.equal(calls, 1);
    assert.equal(reply, "Read page 9. [1]");
  } finally { globalThis.fetch = originalFetch; }
});
