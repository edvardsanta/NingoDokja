import test from "node:test";
import assert from "node:assert/strict";
import { mediaAttachments, documentAttachments, richEmbeds } from "../messages/documents.js";
import { createDiscordMessageHandler } from "../messages/message_listener.js";
import { toMessageContext } from "../runtime/discordjs_runtime.js";

test("document attachments accept PDFs and plain-text files, rejecting other kinds", () => {
  const common = { url: "https://example.com/paper.pdf", size: 1600000 };
  const expected = { ...common, filename: "paper.pdf", content_type: "application/pdf" };
  assert.deepEqual(documentAttachments([{ ...common, name: "paper.pdf", contentType: "application/pdf" }]), [expected]);
  assert.deepEqual(documentAttachments([expected, { ...common, filename: "photo.png", content_type: "image/png" }]), [expected]);
  assert.deepEqual(documentAttachments([null, { ...common, filename: "paper.pdf", size: "bad" }]), []);
  const text = { url: "https://cdn.example.com/message.txt", filename: "message.txt", content_type: "text/plain", size: 39000 };
  assert.deepEqual(documentAttachments([text]), [text]);
  const typed = { url: "https://cdn.example.com/notes", filename: "notes", content_type: "text/plain", size: 10 };
  assert.deepEqual(documentAttachments([typed, { ...common, filename: "run.sh", content_type: "text/x-shellscript" }]), [typed]);
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
    await createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, locale: "en" }).handler({
      userId: "member", channelId: "channel", messageId: "message", content: "", documentAttachments: [attachment],
      isBot: false, isDirectMessage: false, mentionsBot: false, reply: async value => { reply = value; },
    });
    assert.equal(calls, 1);
    assert.equal(reply, "Read page 9. [1]");
  } finally { globalThis.fetch = originalFetch; }
});

test("an image-only message is dispatched for the vision workflow", async () => {
  const originalFetch = globalThis.fetch;
  const attachment = { url: "https://example.com/shot.png", filename: "shot.png", content_type: "image/png", size: 1600000 };
  let calls = 0;
  globalThis.fetch = (async (input, init) => {
    calls++;
    assert.equal(input, "http://orch");
    const request = JSON.parse(String(init?.body));
    assert.equal(request.payload.content, "");
    assert.deepEqual(request.payload.mediaAttachments, [attachment]);
    return new Response(JSON.stringify({ status: "ok", reply: "Ridiculous price." }));
  }) as typeof fetch;
  try {
    let reply: unknown;
    await createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, locale: "en" }).handler({
      userId: "member", channelId: "channel", messageId: "message", content: "", mediaAttachments: [attachment],
      isBot: false, isDirectMessage: false, mentionsBot: false, reply: async value => { reply = value; },
    });
    assert.equal(calls, 1);
    assert.equal(reply, "Ridiculous price.");
  } finally { globalThis.fetch = originalFetch; }
});

test("a message with no content and no attachments is still ignored", async () => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = (async () => { calls++; return new Response("{}"); }) as typeof fetch;
  try {
    await createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, locale: "en" }).handler({
      userId: "member", channelId: "channel", messageId: "message", content: "",
      isBot: false, isDirectMessage: false, mentionsBot: false, reply: async () => {},
    });
    assert.equal(calls, 0);
  } finally { globalThis.fetch = originalFetch; }
});

test("pictures and videos project as metadata and everything else is left out", () => {
  const common = { url: "https://example.com/a", size: 10 };
  assert.deepEqual(mediaAttachments([
    { ...common, name: "clip.mp4", contentType: "video/mp4" },
    { ...common, filename: "meme.png", content_type: "IMAGE/PNG" },
    { ...common, name: "paper.pdf", contentType: "application/pdf" },
    { ...common, name: "bad.png", contentType: "image/png", size: "big" },
    null,
  ]), [
    { ...common, filename: "clip.mp4", content_type: "video/mp4" },
    { ...common, filename: "meme.png", content_type: "image/png" },
  ]);
  assert.equal(mediaAttachments(Array.from({ length: 9 }, (_, i) => ({ ...common, name: `${i}.png`, contentType: "image/png" }))).length, 5);
});

test("the Discord runtime hands over the media of a message and the message it answers", async () => {
  const video = { url: "https://example.com/clip.mp4", name: "clip.mp4", contentType: "video/mp4", size: 10 };
  const referenced = {
    id: "meme-message", channelId: "channel", guildId: "guild", content: "#Old",
    author: { id: "webhook-id", bot: true, username: "Ningo Dokja" }, attachments: new Map([["file", video]]),
  };
  const context = await toMessageContext({
    author: { id: "member", bot: false }, id: "message", channelId: "channel", guildId: "guild",
    content: "#SomeGames", channel: { type: 0 }, mentions: { users: new Map() }, attachments: new Map(),
    reference: { messageId: "meme-message", channelId: "channel" }, fetchReference: async () => referenced,
  } as any);
  assert.deepEqual(context.mediaAttachments, []);
  assert.deepEqual(context.replyTo, {
    messageId: "meme-message", authorId: "webhook-id", authorName: "Ningo Dokja", isBot: true, content: "#Old",
    mediaAttachments: [{ url: video.url, filename: "clip.mp4", content_type: "video/mp4", size: 10 }],
  });
});

test("a message sends its media and the answered message to the orchestrator for a tag proposal", async () => {
  const originalFetch = globalThis.fetch;
  const media = [{ url: "https://example.com/clip.mp4", filename: "clip.mp4", content_type: "video/mp4", size: 10 }];
  const replyTo = { messageId: "m0", authorId: "u2", authorName: "Alex", isBot: false, content: "x".repeat(3000), mediaAttachments: media };
  let seen: any;
  globalThis.fetch = (async (_input, init) => {
    seen = JSON.parse(String(init?.body));
    return new Response(JSON.stringify({ status: "ok", reply: "" }));
  }) as typeof fetch;
  try {
    let replied = false;
    await createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000 }).handler({
      userId: "member", channelId: "channel", messageId: "message", content: "#SomeGames", isBot: false, isDirectMessage: false,
      mentionsBot: false, mediaAttachments: media, replyTo, reply: async () => { replied = true; },
    });
    assert.deepEqual(seen.payload.mediaAttachments, media);
    assert.equal(seen.payload.replyTo.messageId, "m0");
    assert.equal(seen.payload.replyTo.content.length, 2000);
    assert.equal(replied, false, "a proposal is never answered");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a post by a webhook, which is how Ningo posts a meme, is never read as a proposal", async () => {
  let calls = 0;
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async () => { calls++; return new Response("{}"); }) as typeof fetch;
  try {
    await createDiscordMessageHandler({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000 }).handler({
      userId: "webhook-id", channelId: "channel", messageId: "message", content: "#TiagoGames", isBot: true, isDirectMessage: false,
      mentionsBot: false, mediaAttachments: [{ url: "https://example.com/a.mp4", filename: "a.mp4", content_type: "video/mp4", size: 1 }], reply: async () => {},
    });
    assert.equal(calls, 0);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("richEmbeds keeps author, title and description, skips empty and bounds at five", () => {
  const values = [
    { author: { name: "Pedro" }, title: "Ghost of Tsushima DIRECTOR'S CUT", description: "Jogando há 2 horas" },
    { title: "Link preview only" },
    {},
    "not-an-embed",
    ...Array.from({ length: 8 }, (_, i) => ({ author: { name: `a${i}` }, title: `t${i}` })),
  ];
  const got = richEmbeds(values);
  assert.equal(got.length, 5);
  assert.deepEqual(got[0], { author: "Pedro", title: "Ghost of Tsushima DIRECTOR'S CUT", description: "Jogando há 2 horas" });
  assert.equal(got[1].title, "Link preview only");
  assert.equal(got[1].author, undefined);
});
