import assert from "node:assert/strict";
import test from "node:test";

import { RecentKeys } from "../messages/recent_keys.js";
import { accountJid, parseJid, toMessageContext, type InboundDependencies, type InboundMessage } from "../runtime/inbound.js";

const ownPn = "5511900000001";
const ownLid = "99887766554433";
const friendPn = "5511987654321";
const friendLid = "11223344556677";

type Sent = { chatId: string; text: string; messageId: string; quoted?: InboundMessage };

function setup(overrides: Partial<InboundDependencies> = {}) {
  const sent: Sent[] = [];
  const presence: Array<[string, boolean]> = [];
  const deps: InboundDependencies = {
    ownJids: () => [`${ownPn}:12@s.whatsapp.net`, `${ownLid}:12@lid`],
    sent: new RecentKeys(),
    send: async (chatId, text, options) => {
      sent.push({ chatId, text, ...options });
    },
    presence: async (chatId, active) => {
      presence.push([chatId, active]);
    },
    newMessageId: () => "BOTID1",
    now: () => 1_800_000_000_000,
    ...overrides,
  };
  return { deps, sent, presence };
}

const direct = (overrides: Partial<InboundMessage["key"]> = {}, text = "hello"): InboundMessage => ({
  key: { id: "m1", remoteJid: `${friendPn}@s.whatsapp.net`, fromMe: false, ...overrides },
  message: { conversation: text },
  messageTimestamp: 1_799_999_990,
});

test("jids are split into account and server, and the device is dropped", () => {
  assert.deepEqual(parseJid(`${ownPn}:12@s.whatsapp.net`), { user: ownPn, server: "s.whatsapp.net" });
  assert.deepEqual(parseJid("120363000000000001@g.us"), { user: "120363000000000001", server: "g.us" });
  assert.equal(parseJid("nonsense"), null);
  assert.equal(parseJid(undefined), null);
  assert.equal(accountJid(`${ownPn}:12@s.whatsapp.net`), `${ownPn}@s.whatsapp.net`);
});

test("a direct message becomes a context with the sender's number", async () => {
  const { deps } = setup();

  const context = await toMessageContext(direct(), deps);

  assert.equal(context.text, "hello");
  assert.equal(context.chatKind, "direct");
  assert.equal(context.chatId, `${friendPn}@s.whatsapp.net`);
  assert.equal(context.senderId, `${friendPn}@s.whatsapp.net`);
  assert.deepEqual(context.senderNumbers, [friendPn]);
  assert.equal(context.fromMe, false);
  assert.equal(context.isSelfChat, false);
  assert.equal(context.timestampMs, 1_799_999_990_000);
});

test("a message from an anonymous id uses the number delivered with it", async () => {
  const { deps } = setup();

  const context = await toMessageContext(
    direct({ remoteJid: `${friendLid}@lid`, remoteJidAlt: `${friendPn}@s.whatsapp.net` }),
    deps,
  );

  assert.equal(context.chatId, `${friendLid}@lid`);
  assert.deepEqual(context.senderNumbers, [friendPn]);
});

test("an anonymous id without a number is looked up, and stays unknown when the lookup fails", async () => {
  const lookups: string[] = [];
  const known = setup({
    resolveNumber: async (jid) => {
      lookups.push(jid);
      return `${friendPn}:3@s.whatsapp.net`;
    },
  });
  const unknown = setup({
    resolveNumber: async () => {
      throw new Error("no mapping");
    },
  });

  const resolved = await toMessageContext(direct({ remoteJid: `${friendLid}:5@lid` }), known.deps);
  const failed = await toMessageContext(direct({ remoteJid: `${friendLid}@lid` }), unknown.deps);

  assert.deepEqual(lookups, [`${friendLid}@lid`]);
  assert.deepEqual(resolved.senderNumbers, [friendPn]);
  assert.deepEqual(failed.senderNumbers, []);
});

test("the account's own chat is recognised by either of its ids", async () => {
  const { deps } = setup();

  const byNumber = await toMessageContext(direct({ remoteJid: `${ownPn}@s.whatsapp.net`, fromMe: true }), deps);
  const byAnonymousId = await toMessageContext(direct({ remoteJid: `${ownLid}@lid`, fromMe: true }), deps);
  const other = await toMessageContext(direct({ fromMe: true }), deps);

  assert.equal(byNumber.isSelfChat, true);
  assert.equal(byNumber.fromMe, true);
  assert.equal(byAnonymousId.isSelfChat, true);
  assert.equal(other.isSelfChat, false);
});

test("status updates, broadcast lists and channels are not chats", async () => {
  const { deps } = setup();

  for (const remoteJid of ["status@broadcast", "120363000000000002@newsletter", "123@broadcast"]) {
    assert.equal((await toMessageContext(direct({ remoteJid }), deps)).chatKind, "other");
  }
});

test("a group message takes its sender from the participant and is quoted when answered", async () => {
  const { deps, sent } = setup();
  const message: InboundMessage = {
    key: { id: "g1", remoteJid: "120363000000000001@g.us", participant: `${friendLid}:7@lid`, participantAlt: `${friendPn}@s.whatsapp.net` },
    message: { conversation: "hi group" },
  };

  const context = await toMessageContext(message, deps);
  await context.reply("answer");

  assert.equal(context.chatKind, "group");
  assert.equal(context.senderId, `${friendLid}@lid`);
  assert.deepEqual(context.senderNumbers, [friendPn]);
  assert.equal(context.isSelfChat, false);
  assert.deepEqual(sent, [{ chatId: "120363000000000001@g.us", text: "answer", messageId: "BOTID1", quoted: message }]);
});

test("a direct reply is not quoted", async () => {
  const { deps, sent } = setup();

  const context = await toMessageContext(direct(), deps);
  await context.reply("answer");

  assert.deepEqual(sent, [{ chatId: `${friendPn}@s.whatsapp.net`, text: "answer", messageId: "BOTID1" }]);
});

test("a reply is remembered before it is sent, so its echo is recognised", async () => {
  const sent = new RecentKeys();
  let knownWhileSending = false;
  const { deps } = setup({
    sent,
    send: async (_chat, _text, options) => {
      knownWhileSending = sent.has(options.messageId);
    },
  });

  const context = await toMessageContext(direct(), deps);
  await context.reply("answer");
  const echo = await toMessageContext(direct({ id: "BOTID1", fromMe: true }), deps);

  assert.equal(knownWhileSending, true);
  assert.equal(echo.sentByBot, true);
  assert.equal(context.sentByBot, false);
});

test("the id a message really went out with is remembered as well", async () => {
  const sent = new RecentKeys();
  const { deps } = setup({ sent, send: async () => ({ key: { id: "REAL-ID" } }) });

  const context = await toMessageContext(direct(), deps);
  const result = await context.reply("answer");

  assert.deepEqual(result, { key: { id: "REAL-ID" } });
  assert.equal(sent.has("BOTID1"), true);
  assert.equal(sent.has("REAL-ID"), true);
});

test("typing is shown in the chat the message came from", async () => {
  const { deps, presence } = setup();

  const context = await toMessageContext(direct(), deps);
  await context.typing(true);
  await context.typing(false);

  assert.deepEqual(presence, [[`${friendPn}@s.whatsapp.net`, true], [`${friendPn}@s.whatsapp.net`, false]]);
});

test("tagging the account tags the owner, not the bot: it is no call, and the tag leaves the text", async () => {
  const { deps } = setup();
  const mention = (jid: string, text: string): InboundMessage => ({
    key: { id: "g1", remoteJid: "120363000000000001@g.us", participant: `${friendPn}@s.whatsapp.net` },
    message: { extendedTextMessage: { text, contextInfo: { mentionedJid: [jid] } } },
  });

  const byNumber = await toMessageContext(mention(`${ownPn}@s.whatsapp.net`, `@${ownPn} what is stoicism?`), deps);
  const byAnonymousId = await toMessageContext(mention(`${ownLid}@lid`, `hey @${ownLid}  what is stoicism?`), deps);
  const someoneElse = await toMessageContext(mention(`${friendPn}@s.whatsapp.net`, `@${friendPn} what is stoicism?`), deps);

  assert.equal(byNumber.mentionsBot, false);
  assert.equal(byNumber.text, "what is stoicism?");
  assert.equal(byAnonymousId.mentionsBot, false);
  assert.equal(byAnonymousId.text, "hey what is stoicism?");
  assert.equal(someoneElse.mentionsBot, false);
  assert.equal(someoneElse.text, `@${friendPn} what is stoicism?`);
});

test("a reply to one of the bot's own messages counts as a mention", async () => {
  const sent = new RecentKeys();
  sent.add("BOT-EARLIER");
  const { deps } = setup({ sent });
  const reply = (stanzaId: string): InboundMessage => ({
    key: { id: "g1", remoteJid: "120363000000000001@g.us", participant: `${friendPn}@s.whatsapp.net` },
    message: { extendedTextMessage: { text: "and then?", contextInfo: { stanzaId } } },
  });

  assert.equal((await toMessageContext(reply("BOT-EARLIER"), deps)).mentionsBot, true);
  assert.equal((await toMessageContext(reply("SOMEONE-ELSES"), deps)).mentionsBot, false);
});

test("text wrapped for disappearing messages is read, and media without text is empty", async () => {
  const { deps } = setup();
  const wrapped: InboundMessage = {
    ...direct(),
    message: { ephemeralMessage: { message: { extendedTextMessage: { text: "ephemeral hello" } } } },
  };
  const media: InboundMessage = { ...direct(), message: { imageMessage: { caption: "a picture" } } };
  const empty: InboundMessage = { ...direct(), message: null };

  assert.equal((await toMessageContext(wrapped, deps)).text, "ephemeral hello");
  assert.equal((await toMessageContext(media, deps)).text, "");
  assert.equal((await toMessageContext(empty, deps)).text, "");
});

test("the timestamp may be a number, a string or a long, and defaults to now", async () => {
  const { deps } = setup();
  const at = (messageTimestamp: InboundMessage["messageTimestamp"]) => ({ ...direct(), messageTimestamp });

  assert.equal((await toMessageContext(at(1_700_000_000), deps)).timestampMs, 1_700_000_000_000);
  assert.equal((await toMessageContext(at("1700000000"), deps)).timestampMs, 1_700_000_000_000);
  assert.equal((await toMessageContext(at({ toNumber: () => 1_700_000_000 }), deps)).timestampMs, 1_700_000_000_000);
  assert.equal((await toMessageContext(at(undefined), deps)).timestampMs, 1_800_000_000_000);
});

test("a message without an id or a chat cannot be read", async () => {
  const { deps } = setup();

  await assert.rejects(toMessageContext(direct({ id: undefined }), deps), /incomplete/);
  await assert.rejects(toMessageContext(direct({ remoteJid: undefined }), deps), /incomplete/);
});
