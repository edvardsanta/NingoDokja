import assert from "node:assert/strict";
import test from "node:test";

import type { Conversation } from "../config.js";
import type { HistoryStore } from "../history/store.js";
import type { WhatsAppMessageContext } from "../messages/message_handler.js";
import { aliasFor, createRecorder } from "../messages/recorder.js";

const conversations: Conversation[] = [
  { alias: "friend", number: "5511987654321" },
  { alias: "team", group: "120363000000000001@g.us" },
];

const context = (overrides: Partial<WhatsAppMessageContext> = {}): WhatsAppMessageContext => ({
  text: "hello",
  messageId: "m1",
  chatId: "5511987654321@s.whatsapp.net",
  chatKind: "direct",
  senderId: "5511987654321@s.whatsapp.net",
  senderNumbers: ["5511987654321"],
  fromMe: false,
  isSelfChat: false,
  sentByBot: false,
  mentionsBot: false,
  timestampMs: 1_800_000_000_000,
  reply: async () => undefined,
  typing: async () => undefined,
  ...overrides,
});

function fakeStore() {
  const appended: Array<{ alias: string; id: string; at: number; who: string; text: string }> = [];
  let failure: Error | undefined;
  const store = {
    append: async (alias: string, entry: { id: string; at: number; who: string; text: string }) => {
      if (failure) throw failure;
      appended.push({ alias, ...entry });
    },
  } as unknown as HistoryStore;
  return { store, appended, fail: (error: Error) => (failure = error) };
}

test("a conversation is found by its group id or by its number, and nothing else is", () => {
  assert.equal(aliasFor(conversations, context()), "friend");
  assert.equal(aliasFor(conversations, context({ senderNumbers: ["551187654321"] })), "friend", "the ninth digit may be missing");
  assert.equal(aliasFor(conversations, context({ chatId: "120363000000000001@g.us", chatKind: "group", senderNumbers: ["5511955555555"] })), "team");
  assert.equal(aliasFor(conversations, context({ senderNumbers: ["5511911111111"] })), undefined);
  assert.equal(aliasFor(conversations, context({ chatId: "120363000000000009@g.us", chatKind: "group" })), undefined);
  // A member of the named group writing to someone else is not that group.
  assert.equal(aliasFor(conversations, context({ chatKind: "group", chatId: "120363000000000009@g.us", senderNumbers: ["5511987654321"] })), undefined);
});

test("what arrives in a named conversation is recorded with who wrote it, answered or not", async () => {
  const { store, appended } = fakeStore();
  const recorder = createRecorder(conversations, store);

  await recorder.inbound(context({ messageId: "a" }));
  await recorder.inbound(context({ messageId: "b", fromMe: true }));
  await recorder.inbound(context({ messageId: "c", sentByBot: true }));
  await recorder.inbound(context({ messageId: "d", chatKind: "group", chatId: "120363000000000001@g.us" }));

  assert.deepEqual(appended.map((entry) => [entry.alias, entry.id, entry.who]), [
    ["friend", "a", "contact"],
    ["friend", "b", "owner"],
    ["friend", "c", "bot"],
    ["team", "d", "member"],
  ]);
});

test("a conversation the owner did not name leaves no trace, and an empty message is not recorded", async () => {
  const { store, appended } = fakeStore();
  const recorder = createRecorder(conversations, store);

  await recorder.inbound(context({ senderNumbers: ["5511911111111"], chatId: "5511911111111@s.whatsapp.net" }));
  await recorder.inbound(context({ text: "  " }));
  await recorder.outbound(context({ senderNumbers: ["5511911111111"] }), "reply", "sent-1");

  assert.deepEqual(appended, []);
});

test("what the bot sends is recorded as the bot, with the id it went out with", async () => {
  const { store, appended } = fakeStore();
  const recorder = createRecorder(conversations, store, { now: () => 5 });

  await recorder.outbound(context(), "an answer", "3EB0ABC");
  await recorder.outbound(context(), "another", undefined);

  assert.deepEqual(appended.map((entry) => [entry.id.startsWith("out-") ? "generated" : entry.id, entry.who, entry.text]), [
    ["3EB0ABC", "bot", "an answer"],
    ["generated", "bot", "another"],
  ]);
});

test("a history that cannot be written is reported without the text and never throws", async () => {
  const { store, fail } = fakeStore();
  fail(new Error("disk full"));
  const errors: string[] = [];
  const recorder = createRecorder(conversations, store, { error: (...args) => errors.push(args.join(" ")) });

  await recorder.inbound(context({ text: "a private message" }));

  assert.equal(errors.length, 1);
  assert.match(errors[0], /could not record a message.*disk full/);
  assert.doesNotMatch(errors[0], /private/);
});
