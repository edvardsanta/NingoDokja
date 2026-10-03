import test from "node:test";
import assert from "node:assert/strict";
import { handleRequest } from "../src/main/ipc.js";
import { MediaGate } from "../src/main/media_gate.js";
import { questionFor } from "../src/main/dialogs.js";
import { OrchestratorClient } from "../src/main/orchestrator_client.js";
import { startReplyServer, ok as wireOk } from "./support/reply_server.js";

const ok = (result: unknown) => ({ status: "ok", result });
const limits = { defaultTimeoutMs: 15000, maxTimeoutMs: 60000 };
const here = { recent: () => true };
const send = { type: "communications.send", payload: { channel_id: "123", content: "hello", all: true, force_nsfw: true, mark_sent: true } };

test("sending needs real input and native confirmation, and targets only the selected channel", async () => {
  const calls: unknown[] = [];
  const orchestrator = { request: async (_type: string, payload: unknown) => { calls.push(payload); return ok({ domain: "system", result: { sent_to: ["123"] } }); } };
  for (const presence of [undefined, { recent: () => false }]) {
    assert.equal((await handleRequest(send, orchestrator, limits, undefined, presence, async () => true)).ok, false);
  }
  assert.equal((await handleRequest(send, orchestrator, limits, undefined, here, async () => false)).ok, false);
  assert.equal(calls.length, 0);
  const result = await handleRequest(send, orchestrator, limits, undefined, here, async (_type, payload) => {
    assert.deepEqual(payload, { channel_ids: ["123"], content: "hello" }); return true;
  });
  assert.deepEqual(result, { ok: true, result: { sent: true, skipped: false } });
  assert.equal(calls.length, 1);
  const question = questionFor("pt", "communications.send", { channel_ids: ["123"], content: "hello" });
  assert.match(question!.detail, /123[\s\S]*hello/);
  assert.equal(question!.accept, "Enviar");
});

test("an attachment must have been listed by the orchestrator", async () => {
  const gate = new MediaGate();
  let called = 0;
  const orchestrator = { request: async () => { called++; return ok({ domain: "system", result: { sent_to: ["123"] } }); } };
  const request = { ...send, payload: { ...send.payload, attachment_url: "https://images.example/meme.png" } };
  assert.equal((await handleRequest(request, orchestrator, limits, gate, here, async () => true)).ok, false);
  assert.equal(called, 0);
  gate.remember([request.payload.attachment_url]);
  assert.equal((await handleRequest(request, orchestrator, limits, gate, here, async () => true)).ok, true);
  assert.equal(called, 1);
});

test("history strips upstream metadata and limits reply fields", async () => {
  let payload: unknown;
  const orchestrator = { request: async (_type: string, raw: unknown) => {
    payload = raw;
    return ok({ domain: "communications", result: { channel_id: "123", before: "", token: "hidden", messages: [{ id: "10", author: "Reader", content: "<script>text</script>", email: "hidden", attachments: ["clip.mp4"] }] } });
  } };
  const result = await handleRequest({ type: "communications.history", payload: { channel_id: "123", limit: 100, token: "hidden" } }, orchestrator, limits);
  assert.deepEqual(payload, { channel_id: "123", before: "" });
  assert.equal(JSON.stringify(result).includes("hidden"), false);
  assert.equal(result.ok, true);
});

test("credentials stay in the shell and are sent only to local communications requests", async () => {
  const token = "x".repeat(32);
  for (const options of [{ endpoint: "tcp://127.0.0.1:1" }, { endpoint: "tcp://192.0.2.1:5558", communicationsToken: token }]) {
    await assert.rejects(new OrchestratorClient(options).request("communications.channels", {}, 100), /communications|Communications/);
  }
  const server = await startReplyServer(() => wireOk({}));
  try {
    const client = new OrchestratorClient({ endpoint: server.endpoint, communicationsToken: token });
    await client.request("communications.channels", {}, 2000);
    await client.request("ningo.status", {}, 2000);
    assert.deepEqual(server.requests[0]?.context, { interface: "desktop", communications_token: token });
    assert.deepEqual(server.requests[1]?.context, { interface: "desktop" });
  } finally { await server.close(); }
});
