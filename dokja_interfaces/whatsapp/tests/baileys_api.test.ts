import assert from "node:assert/strict";
import { mkdtemp, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import * as baileys from "baileys";

import { loadAuth, quietLogger } from "../runtime/baileys_socket.js";
import { disconnectReason } from "../runtime/whatsapp_runtime.js";

// Baileys is a release candidate that follows WhatsApp's own changes. These tests fail when an
// upgrade changes what this interface relies on, before the bot is linked to a real account.

test("the Baileys exports used by the interface exist", () => {
  assert.equal(typeof baileys.default, "function");
  assert.equal(typeof baileys.useMultiFileAuthState, "function");
  assert.equal(typeof baileys.makeCacheableSignalKeyStore, "function");
  assert.equal(typeof baileys.Browsers.ubuntu, "function");
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
