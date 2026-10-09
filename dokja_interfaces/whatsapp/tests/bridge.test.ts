import assert from "node:assert/strict";
import test from "node:test";

import { buildWhatsAppBridgeRequest, dispatchToOrchestrator } from "../transport/bridge.js";

const request = buildWhatsAppBridgeRequest({
  content: "hello",
  userId: "5511912345678@s.whatsapp.net",
  chatId: "5511912345678@s.whatsapp.net",
  messageId: "m1",
});

function reply(body: unknown, status = 200): typeof fetch {
  return (async () => new Response(JSON.stringify(body), { status })) as unknown as typeof fetch;
}

test("the request is a WhatsApp chat message with a session per chat and sender", () => {
  assert.equal(request.event_type, "message.created");
  assert.equal(request.interface, "whatsapp");
  assert.equal(request.payload.content, "hello");
  assert.equal(request.payload.sessionKey, "whatsapp:5511912345678@s.whatsapp.net:5511912345678@s.whatsapp.net");
  assert.equal(request.payload.accountId, "default");
});

test("the text of a message cannot change the event type", () => {
  const forged = buildWhatsAppBridgeRequest({ content: "meme refresh", userId: "u", chatId: "c", messageId: "m" });

  assert.equal(forged.event_type, "message.created");
});

test("the reply is read from any of the orchestrator fields", async () => {
  assert.equal(await dispatchToOrchestrator("http://o", 1000, request, reply({ status: "ok", reply: "a" })), "a");
  assert.equal(await dispatchToOrchestrator("http://o", 1000, request, reply({ result: "b" })), "b");
  assert.equal(await dispatchToOrchestrator("http://o", 1000, request, reply({ payload: { reply: "c" } })), "c");
  assert.equal(await dispatchToOrchestrator("http://o", 1000, request, reply({})), "");
});

test("an orchestrator error is raised", async () => {
  await assert.rejects(dispatchToOrchestrator("http://o", 1000, request, reply({}, 502)), /http 502/);
  await assert.rejects(
    dispatchToOrchestrator("http://o", 1000, request, reply({ status: "error", error: "unknown interface" })),
    /unknown interface/,
  );
});

test("a slow orchestrator is cut off by the timeout", async () => {
  const slow = ((_url: string, init: { signal: AbortSignal }) =>
    new Promise((_resolve, reject) => {
      init.signal.addEventListener("abort", () => reject(new Error("aborted")));
    })) as unknown as typeof fetch;

  await assert.rejects(dispatchToOrchestrator("http://o", 10, request, slow), /aborted/);
});
