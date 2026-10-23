import assert from "node:assert/strict";
import { mkdir, mkdtemp, readdir, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import * as baileys from "baileys";

import {
  adaptSocket,
  loadAuth,
  lookupSent,
  newMessageId,
  quietLogger,
  wipeAuth,
  type RawSocket,
} from "../runtime/baileys_socket.js";
import { SentMessages } from "../runtime/sent_messages.js";
import { disconnectReason } from "../runtime/whatsapp_runtime.js";

// Baileys is a release candidate that follows WhatsApp's own changes. These tests fail when an
// upgrade changes what this interface relies on, before the bot is linked to a real account.

test("the Baileys exports used by the interface exist", () => {
  assert.equal(typeof baileys.default, "function");
  assert.equal(typeof baileys.useMultiFileAuthState, "function");
  assert.equal(typeof baileys.makeCacheableSignalKeyStore, "function");
  assert.equal(typeof baileys.Browsers.ubuntu, "function");
  assert.equal(typeof baileys.generateMessageIDV2, "function");
});

test("message ids have the format of the official client's", () => {
  const own = "5511900000001:4@s.whatsapp.net";

  assert.match(newMessageId(own), /^3EB0[0-9A-F]{18}$/);
  assert.match(newMessageId(), /^3EB0[0-9A-F]{18}$/);
  assert.notEqual(newMessageId(own), newMessageId(own));
});

test("the content of what the bot sends is kept so a retry request can be answered", async () => {
  const sent = new SentMessages<unknown>();
  const calls: unknown[][] = [];
  const raw: RawSocket = {
    ev: { on: () => undefined },
    user: { id: "5511900000001:4@s.whatsapp.net" },
    sendMessage: async (...args) => {
      calls.push(args);
      return { key: { id: "SENT1" }, message: { conversation: "answer" } };
    },
    sendPresenceUpdate: async () => undefined,
    logout: async () => undefined,
    end: () => undefined,
  };
  const socket = adaptSocket(raw, sent);

  const result = await socket.sendMessage("5511987654321@s.whatsapp.net", { text: "answer" }, { messageId: "SENT1" });

  assert.deepEqual(result, { key: { id: "SENT1" }, message: { conversation: "answer" } });
  assert.deepEqual(calls, [["5511987654321@s.whatsapp.net", { text: "answer" }, { messageId: "SENT1" }]]);
  assert.deepEqual(lookupSent(sent, "SENT1"), { conversation: "answer" });
  assert.equal(lookupSent(sent, "SOMEONE-ELSES"), undefined);
  assert.equal(lookupSent(sent, undefined), undefined);
  assert.equal(socket.user?.id, "5511900000001:4@s.whatsapp.net");
});

test("the disconnect codes the loop reacts to are the ones Baileys uses", () => {
  assert.equal(disconnectReason.loggedOut, baileys.DisconnectReason.loggedOut);
  assert.equal(disconnectReason.connectionReplaced, baileys.DisconnectReason.connectionReplaced);
  assert.equal(disconnectReason.restartRequired, baileys.DisconnectReason.restartRequired);
});

test("jid parsing agrees with Baileys on the ids the interface reads", () => {
  for (const jid of ["5511987654321:12@s.whatsapp.net", "11223344556677:3@lid", "120363000000000001@g.us"]) {
    const ours = jid.slice(0, jid.lastIndexOf("@")).split(":")[0];
    assert.equal(baileys.jidDecode(jid)?.user, ours);
  }
});

test("the session folder is created readable by its owner only, and an empty one starts unlinked", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "dokja-wa-"));
  try {
    const dir = path.join(root, "nested", "default");

    const auth = await loadAuth(dir);

    assert.equal((await stat(dir)).mode & 0o777, 0o700);
    assert.equal(auth.state.creds.me, undefined);
    assert.equal(typeof auth.saveCreds, "function");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the quiet logger keeps warnings and errors as short text and nothing else", () => {
  const lines: string[] = [];
  const logger = quietLogger((...args) => lines.push(args.join(" ")));

  logger.info({ jid: "5511987654321@s.whatsapp.net" }, "received");
  logger.debug({ key: "secret" }, "details");
  logger.trace({}, "trace");
  logger.warn({ jid: "5511987654321@s.whatsapp.net", key: "secret" }, "connection slow");
  logger.error({ err: { message: "stream errored out" }, key: "secret" }, "failed");
  logger.child({ module: "x" }).error({}, "x".repeat(500));

  assert.equal(lines.length, 3);
  assert.doesNotMatch(lines.join("\n"), /5511987654321|secret/);
  assert.match(lines[1], /failed: stream errored out/);
  assert.ok(JSON.parse(lines[2]).text.length <= 200);
});

test("wiping the session leaves an empty folder that only its owner can read", async () => {
  const root = await mkdtemp(path.join(tmpdir(), "dokja-wa-"));
  try {
    const dir = path.join(root, "default");
    await loadAuth(dir);
    await writeFile(path.join(dir, "creds.json"), "{}");
    await mkdir(path.join(dir, "nested"));
    await writeFile(path.join(dir, "nested", "key.json"), "{}");

    await wipeAuth(dir);

    assert.deepEqual(await readdir(dir), []);
    assert.equal((await stat(dir)).mode & 0o777, 0o700);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
