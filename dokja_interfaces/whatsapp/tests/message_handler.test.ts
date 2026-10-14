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
  assert.deepEqual(replies, ["Hello there."]);
  assert.deepEqual(typing, [true, false]);
});

test("a number the owner typed with the ninth digit still matches the account's id without it", async () => {
  const { handler, dispatched, context } = setup();

  await handler(context({ senderNumbers: ["551187654321"] }));

  assert.equal(dispatched.length, 1);
});

test("a number that is not allowed is ignored without a trace", async () => {
  const { handler, dispatched, replies, logs, context } = setup();

  await handler(context({ senderNumbers: ["5511911111111"] }));
  await handler(context({ senderNumbers: [], messageId: "m2" }));

  assert.equal(dispatched.length, 0);
  assert.deepEqual(replies, []);
  assert.deepEqual(logs, []);
});

test("nobody is answered when the allow list is empty", async () => {
  const { handler, dispatched, context } = setup({ allowedNumbers: [] });

  await handler(context());

  assert.equal(dispatched.length, 0);
});

test("the account's own chat is answered, and only when self chat is on", async () => {
  const on = setup();
  await on.handler(on.selfChat());
  assert.equal(on.dispatched.length, 1);
  assert.deepEqual(on.replies, ["Hello there."]);

  const off = setup({ selfChat: false });
  await off.handler(off.selfChat());
  assert.equal(off.dispatched.length, 0);
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

test("a group is answered only when it is allowed and the bot is mentioned", async () => {
  const { handler, dispatched, replies, group } = setup();

  await handler(group());
  await handler(group({ mentionsBot: false, messageId: "m2" }));
  await handler(group({ chatId: "120363000000000009@g.us", messageId: "m3" }));

  assert.equal(dispatched.length, 1);
  assert.equal(dispatched[0].payload.channelId, "120363000000000001@g.us");
  assert.equal(dispatched[0].payload.userId, "5511955555555@s.whatsapp.net");
  assert.deepEqual(replies, ["Hello there."]);
});

test("a group that is not allowed is reported once so its id can be copied", async () => {
  const { handler, logs, group } = setup();
  const other = "120363000000000009@g.us";

  await handler(group({ chatId: other, messageId: "m1" }));
  await handler(group({ chatId: other, messageId: "m2" }));

  assert.equal(logs.length, 1);
  assert.deepEqual(JSON.parse(logs[0]), { message: "ignored", reason: "policy", conversation: other });
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

  assert.deepEqual(replies, [failureReply]);
  assert.deepEqual(typing, [true, false]);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /dispatch failed/);
  assert.doesNotMatch(errors[0], /stoicism/);
});

test("a long reply is cut under the message size limit", async () => {
  const { handler, replies, context } = setup({}, "x".repeat(maxReplyLength * 3));

  await handler(context());

  assert.equal(replies[0].length, maxReplyLength);
});

test("an empty reply sends nothing", async () => {
  const { handler, replies, context } = setup({}, "  ");

  await handler(context());

  assert.deepEqual(replies, []);
});

test("a presence update that fails does not stop the answer", async () => {
  const { handler, replies, context } = setup();

  await handler(context({ typing: async () => Promise.reject(new Error("presence")) }));

  assert.deepEqual(replies, ["Hello there."]);
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
