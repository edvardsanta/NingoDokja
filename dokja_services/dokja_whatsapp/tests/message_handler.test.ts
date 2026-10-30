import assert from "node:assert/strict";
import test from "node:test";

import {
  createWhatsAppMessageHandler,
  failureReply,
  maxMessageAgeMs,
  maxReplyLength,
  type WhatsAppMessageConfig,
  type WhatsAppMessageContext,
} from "../messages/message_handler.js";

// Every reply Ningo writes on WhatsApp is signed, so the owner's chats tell its words from the owner's.
const SIGNED = "\n\nAssinado _Ningo Dokja_";
import type { WhatsAppBridgeRequest } from "../transport/bridge.js";

const owner = "5511900000001";
const friend = "5511987654321";
const nowMs = 1_800_000_000_000;

const config: WhatsAppMessageConfig = {
  orchestratorEndpoint: "http://orchestrator.test/orchestrator",
  orchestratorTimeoutMs: 1000,
  selfChat: true,
  allowedNumbers: [friend],
  allowedGroups: ["120363000000000001@g.us"],
};

function setup(
  overrides: Partial<WhatsAppMessageConfig> = {},
  answer: string | Error = "Hello there.",
  answerAfterMs = 0,
) {
  const dispatched: WhatsAppBridgeRequest[] = [];
  const logs: string[] = [];
  const errors: string[] = [];
  const handler = createWhatsAppMessageHandler({ ...config, ...overrides }, {
    typingDelayMs: 5,
    dispatch: async (_endpoint, _timeout, body) => {
      dispatched.push(body);
      if (answerAfterMs > 0) {
        await new Promise((resolve) => setTimeout(resolve, answerAfterMs));
      }
      if (answer instanceof Error) {
        throw answer;
      }
      return answer;
    },
    now: () => nowMs,
    log: (...args) => logs.push(args.join(" ")),
    error: (...args) => errors.push(args.join(" ")),
  });
  const replies: string[] = [];
  const typing: boolean[] = [];

  // A direct message from an allowed friend, unless overridden.
  const context = (overrides: Partial<WhatsAppMessageContext> = {}): WhatsAppMessageContext => ({
    text: "what is stoicism?",
    messageId: "m1",
    chatId: `${friend}@s.whatsapp.net`,
    chatKind: "direct",
    senderId: `${friend}@s.whatsapp.net`,
    senderNumbers: [friend],
    fromMe: false,
    isSelfChat: false,
    sentByBot: false,
    mentionsBot: false,
    timestampMs: nowMs - 1000,
    reply: async (text) => replies.push(text),
    typing: async (active) => {
      typing.push(active);
    },
    ...overrides,
  });
  const selfChat = (overrides: Partial<WhatsAppMessageContext> = {}) =>
    context({
      chatId: `${owner}@s.whatsapp.net`,
      senderId: `${owner}@s.whatsapp.net`,
      senderNumbers: [owner],
      fromMe: true,
      isSelfChat: true,
      ...overrides,
    });
  const group = (overrides: Partial<WhatsAppMessageContext> = {}) =>
    context({
      chatId: "120363000000000001@g.us",
      chatKind: "group",
      senderId: "5511955555555@s.whatsapp.net",
      senderNumbers: ["5511955555555"],
      mentionsBot: true,
      ...overrides,
    });
  return { handler, dispatched, replies, typing, logs, errors, context, selfChat, group };
}

test("a message from an allowed number goes to the orchestrator and the reply comes back", async () => {
  const { handler, dispatched, replies, typing, context } = setup({}, "Hello there.", 40);

  await handler(context());

  assert.equal(dispatched.length, 1);
  assert.equal(dispatched[0].event_type, "message.created");
  assert.equal(dispatched[0].interface, "whatsapp");
  assert.equal(dispatched[0].payload.content, "what is stoicism?");
  assert.equal(dispatched[0].payload.channelId, `${friend}@s.whatsapp.net`);
  assert.equal(dispatched[0].payload.sessionKey, `whatsapp:${friend}@s.whatsapp.net:${friend}@s.whatsapp.net`);
  assert.deepEqual(replies, ["Hello there." + SIGNED]);
  assert.deepEqual(typing, [true, false]);
});

test("a number the owner typed with the ninth digit still matches the account's id without it", async () => {
  const { handler, dispatched, context } = setup();

  await handler(context({ senderNumbers: ["551187654321"] }));

  assert.equal(dispatched.length, 1);
});

test("direct sender numbers are forwarded for domain authorization", async () => {
  const { handler, dispatched, replies, logs, context } = setup();

  await handler(context({ senderNumbers: ["5511911111111"] }));
  await handler(context({ senderNumbers: [], messageId: "m2" }));

  assert.equal(dispatched.length, 2);
  assert.deepEqual(dispatched.map((request) => request.payload.senderNumbers), [["5511911111111"], []]);
  assert.equal(replies.length, 2);
  assert.ok(logs.every((line) => !line.includes("5511911111111")));
});

test("the gateway forwards direct messages even with an empty local allowlist", async () => {
  const { handler, dispatched, context } = setup({ allowedNumbers: [] });

  await handler(context());

  assert.equal(dispatched.length, 1);
});

test("self-chat identity is sent to the domain regardless of local policy", async () => {
  const on = setup();
  await on.handler(on.selfChat());
  assert.equal(on.dispatched.length, 1);
  assert.deepEqual(on.replies, ["Hello there." + SIGNED]);

  const off = setup({ selfChat: false });
  await off.handler(off.selfChat());
  assert.equal(off.dispatched.length, 1);
  assert.equal(off.dispatched[0].payload.isSelfChat, true);
});

test("the echo of the bot's own reply is not answered again", async () => {
  const { handler, dispatched, selfChat } = setup();

  await handler(selfChat({ sentByBot: true }));

  assert.equal(dispatched.length, 0);
});

test("what the owner types in other chats is never answered", async () => {
  const { handler, dispatched, context } = setup();

  await handler(context({ fromMe: true }));

  assert.equal(dispatched.length, 0);
});

test("a message from a status update, broadcast list or channel is ignored", async () => {
  const { handler, dispatched, context } = setup();

  await handler(context({ chatKind: "other" }));

  assert.equal(dispatched.length, 0);
});

test("a late message is not answered", async () => {
  const { handler, dispatched, context } = setup();

  await handler(context({ timestampMs: nowMs - maxMessageAgeMs - 1 }));
  await handler(context({ timestampMs: nowMs - maxMessageAgeMs, messageId: "m2" }));

  assert.equal(dispatched.length, 1);
});

test("group id and mention state are forwarded for domain authorization", async () => {
  const { handler, dispatched, replies, group } = setup();

  await handler(group());
  await handler(group({ mentionsBot: false, messageId: "m2" }));
  await handler(group({ chatId: "120363000000000009@g.us", messageId: "m3" }));

  assert.equal(dispatched.length, 3);
  assert.equal(dispatched[0].payload.channelId, "120363000000000001@g.us");
  assert.equal(dispatched[0].payload.userId, "5511955555555@s.whatsapp.net");
  assert.deepEqual(dispatched.map((request) => request.payload.mentionsBot), [true, false, true]);
  assert.equal(replies.length, 3);
});

test("a group that is not allowed is reported once so its id can be copied", async () => {
  const { handler, logs, group } = setup();
  const other = "120363000000000009@g.us";

  await handler(group({ chatId: other, messageId: "m1" }));
  await handler(group({ chatId: other, messageId: "m2" }));

  const notices = logs.map((line) => JSON.parse(line)).filter((line) => line.reason === "policy");
  assert.equal(notices.length, 1);
  assert.deepEqual(notices[0], { message: "ignored", reason: "policy", conversation: other });
});

test("an empty message is ignored", async () => {
  const { handler, dispatched, context } = setup();

  await handler(context({ text: "   " }));

  assert.equal(dispatched.length, 0);
});

test("the same message delivered twice is answered once", async () => {
  const { handler, dispatched, replies, context } = setup();

  await handler(context());
  await handler(context());

  assert.equal(dispatched.length, 1);
  assert.equal(replies.length, 1);
});

test("a failing orchestrator is answered with a short apology and logged without the text", async () => {
  const { handler, replies, typing, errors, context } = setup({}, new Error("orchestrator http 502"), 40);

  await handler(context());

  assert.deepEqual(replies, [failureReply + SIGNED]);
  assert.deepEqual(typing, [true, false]);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /dispatch failed/);
  assert.doesNotMatch(errors[0], /stoicism/);
});

test("a long reply is cut under the message size limit", async () => {
  const { handler, replies, context } = setup({}, "x".repeat(maxReplyLength * 3));

  await handler(context());

  assert.equal(replies[0].length, maxReplyLength);
  assert.ok(replies[0].endsWith(SIGNED), "the signature survives the cut");
});

test("an empty reply sends nothing", async () => {
  const { handler, replies, context } = setup({}, "  ");

  await handler(context());

  assert.deepEqual(replies, []);
});

test("a presence update that fails does not stop the answer", async () => {
  const { handler, replies, context } = setup();

  await handler(context({ typing: async () => Promise.reject(new Error("presence")) }));

  assert.deepEqual(replies, ["Hello there." + SIGNED]);
});

test("the log never carries the message text or a phone number", async () => {
  const { handler, logs, context } = setup();

  await handler(context());

  const all = logs.join("\n");
  assert.doesNotMatch(all, /stoicism/);
  assert.doesNotMatch(all, new RegExp(friend));
});

test("an answer that comes at once never shows typing, so a switched-off interface stays quiet", async () => {
  const { handler, typing, context } = setup({}, "");

  await handler(context());
  await new Promise((resolve) => setTimeout(resolve, 30));

  assert.deepEqual(typing, []);
});

test("the owner calls Ningo by name in an allowed group too, and nothing else they write is answered", async () => {
  const { handler, dispatched, group } = setup();
  const own = (text: string, messageId: string, overrides: Partial<WhatsAppMessageContext> = {}) =>
    group({ text, messageId, fromMe: true, mentionsBot: false, senderId: `${owner}@s.whatsapp.net`, senderNumbers: [owner], ...overrides });

  await handler(own("Ningo, what is justice?", "m1"));
  await handler(own("  /NINGO   and power?", "m2"));
  await handler(own("ningo,quick one", "m3"));
  await handler(own("just chatting with friends", "m4"));
  await handler(own("Ningo is great", "m5"));
  await handler(own("/ningoX not a call", "m6"));
  await handler(own("Ningo,", "m7"));
  await handler(own("Ningo, in another group", "m8", { chatId: "120363000000000009@g.us" }));
  await handler(own("Ningo, an echo of the bot", "m9", { sentByBot: true }));

  assert.deepEqual(dispatched.map((request) => request.payload.content), ["what is justice?", "and power?", "quick one"]);
  const allowed = group().chatId;
  assert.ok(dispatched.every((request) => request.payload.mentionsBot === true && request.payload.channelId === allowed));
});

test("in an allowed group any member calls Ningo by name, and only there", async () => {
  const { handler, dispatched, group } = setup();
  await handler(group({ text: "Ningo, what would Kant say?", mentionsBot: false, messageId: "m1" }));
  await handler(group({ text: "/ningo and Hegel?", mentionsBot: false, messageId: "m2" }));
  await handler(group({ text: "Ningo, elsewhere", mentionsBot: false, messageId: "m3", chatId: "120363000000000009@g.us" }));
  await handler(group({ text: "just talking", mentionsBot: false, messageId: "m4" }));
  assert.deepEqual(dispatched.map((request) => [request.payload.content, request.payload.mentionsBot]), [
    ["what would Kant say?", true],
    ["and Hegel?", true],
    ["Ningo, elsewhere", false],
    ["just talking", false],
  ]);
});

test("the orchestrator is told when a call comes from the linked account, and only then", async () => {
  const { handler, dispatched, group } = setup();
  await handler(group({ text: "Ningo, who am I?", fromMe: true, mentionsBot: false, messageId: "m1" }));
  await handler(group({ text: "Ningo, who am I?", mentionsBot: false, messageId: "m2" }));
  assert.deepEqual(dispatched.map((request) => request.payload.isOwnAccount), [true, false]);
});
