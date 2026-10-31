import test from "node:test";
import assert from "node:assert/strict";

import { BridgeChat, BridgeMissing } from "../src/main/chat_stream.js";
import { handleRequest } from "../src/main/ipc.js";
import { OrchestratorError, type OrchestratorReply } from "../src/main/orchestrator_client.js";
import { parseConfig } from "../src/main/config.js";

// A bridge that answers with these pieces of text, cut wherever the test likes.
function bridge(pieces: string[], status = 200) {
  const seen: Array<{ url: string; init: RequestInit }> = [];
  const fakeFetch = (async (url: string | URL, init?: RequestInit) => {
    seen.push({ url: String(url), init: init ?? {} });
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        for (const piece of pieces) controller.enqueue(new TextEncoder().encode(piece));
        controller.close();
      },
    });
    return new Response(body, { status });
  }) as typeof fetch;
  return { seen, chat: new BridgeChat({ endpoint: "http://localhost:8091", token: "fixture-token", fetch: fakeFetch }) };
}

const final = JSON.stringify({ status: "ok", result: { event_id: "e", workflow: "chat", domain: "chat", result: { reply: "The answer" } } });

test("a streamed turn hands over each preview, even cut across reads, then returns the reply", async () => {
  const { seen, chat } = bridge(['{"preview":"Th', 'e"}\n{"preview":"The ans"}\n', final.slice(0, 10), final.slice(10) + "\n"]);
  const previews: string[] = [];
  const reply = await chat.stream({ content: "q", conversation_id: "c-1" }, 5_000, (text) => previews.push(text));
  assert.deepEqual(previews, ["The", "The ans"]);
  assert.equal(reply.status, "ok");
  assert.equal(seen[0]?.url, "http://localhost:8091/orchestrator/chat/stream");
  assert.equal((seen[0]?.init.headers as Record<string, string>).Authorization, "Bearer fixture-token");
  const envelope = JSON.parse(String(seen[0]?.init.body));
  assert.equal(envelope.source, "desktop");
  assert.equal(envelope.type, "message.created");
  assert.deepEqual(envelope.payload, { content: "q", conversation_id: "c-1" });
  assert.equal(envelope.context.communications_token, undefined, "the communications credential never travels with a chat turn");
});

test("a refused credential, a stream without a reply and a broken line are said plainly", async () => {
  await assert.rejects(bridge([], 401).chat.stream({}, 5_000, () => {}), (error: unknown) => error instanceof OrchestratorError && /credential/.test(error.message));
  await assert.rejects(bridge(['{"preview":"half"}\n']).chat.stream({}, 5_000, () => {}), /without a reply/);
  await assert.rejects(bridge(["not json\n"]).chat.stream({}, 5_000, () => {}), /not valid JSON/);
});

test("only a Talk turn with a conversation is streamed, and its previews name the conversation", async () => {
  const requests: string[] = [];
  const streamed: Array<Record<string, unknown>> = [];
  const conversation = "c".repeat(32);
  const turn = { id: "d".repeat(32), seq: 1, send_id: "send-0001", content: "q", reply: "The answer", state: "completed", reason: "", attempts: 1 };
  const reply: OrchestratorReply = { status: "ok", result: { domain: "chat", result: { reply: "The answer", turn } } };
  const orchestrator = {
    async request(type: string) {
      requests.push(type);
      return reply;
    },
    async streamChat(payload: Record<string, unknown>, _timeout: number, onPreview: (text: string) => void) {
      streamed.push(payload);
      onPreview("The ans");
      return reply;
    },
  };
  const limits = { defaultTimeoutMs: 1_000, maxTimeoutMs: 120_000 };
  const pressed = { recent: () => true };
  const previews: unknown[] = [];
  const result = await handleRequest({ type: "message.created", payload: { content: "q", conversation_id: conversation, send_id: "send-0001" } }, orchestrator, limits, undefined, pressed, undefined, (preview) => previews.push(preview));
  assert.equal(result.ok, true);
  assert.deepEqual(streamed, [{ content: "q", conversation_id: conversation, send_id: "send-0001" }]);
  assert.deepEqual(previews, [{ conversationId: conversation, text: "The ans" }]);
  assert.deepEqual(requests, []);

  assert.equal((await handleRequest({ type: "message.created", payload: { content: "q" } }, orchestrator, limits, undefined, pressed, undefined, () => {})).ok, false, "a message outside a conversation is refused");
  await handleRequest({ type: "ningo.status" }, orchestrator, limits, undefined, pressed, undefined, () => {});
  assert.deepEqual(requests, ["ningo.status"], "another action uses the request socket");
});

test("the bridge is used only on this machine", () => {
  assert.equal(parseConfig([], { DOKJA_BRIDGE_ENDPOINT: "http://localhost:8091" }, "en").bridgeEndpoint, "http://localhost:8091");
  assert.equal(parseConfig([], { DOKJA_BRIDGE_ENDPOINT: "http://orchestrator.example:8091" }, "en").bridgeEndpoint, undefined);
  assert.equal(parseConfig([], {}, "en").bridgeEndpoint, undefined);
});

test("a long reply whose previews add up past megabytes still lands", async () => {
  const text = "word ".repeat(2000);
  const lines: string[] = [];
  for (let end = 10; end <= text.length; end += 10) lines.push(JSON.stringify({ preview: text.slice(0, end) }) + "\n");
  lines.push(JSON.stringify({ status: "ok", result: { reply: text } }) + "\n");
  let previews = 0;
  const reply = await bridge(lines).chat.stream({}, 5_000, () => previews++);
  assert.equal(previews, 1000);
  assert.deepEqual(reply.result, { reply: text });
});

test("a bridge that never saw the turn says so, so the turn can go over the socket", async () => {
  await assert.rejects(bridge([], 404).chat.stream({}, 5_000, () => {}), BridgeMissing);
  const refusing = new BridgeChat({
    endpoint: "http://localhost:8091",
    token: "fixture-token",
    fetch: (async () => {
      throw new TypeError("fetch failed", { cause: { code: "ECONNREFUSED" } });
    }) as typeof fetch,
  });
  await assert.rejects(refusing.stream({}, 5_000, () => {}), BridgeMissing);
  const broken = new BridgeChat({
    endpoint: "http://localhost:8091",
    token: "fixture-token",
    fetch: (async () => {
      throw new TypeError("fetch failed", { cause: { code: "ECONNRESET" } });
    }) as typeof fetch,
  });
  await assert.rejects(broken.stream({}, 5_000, () => {}), (error: unknown) => error instanceof OrchestratorError && error.code === "unavailable", "a turn that may have run is never sent twice");
});

test("where a send stands is handed on, and a state this build does not know is left alone", async () => {
  const lines = ['{"progress":"queued"}', '{"progress":"somewhere-new"}', '{"progress":"dispatched"}', '{"preview":"The ans"}', '{"status":"ok","result":{}}'];
  const body = new Response(`${lines.join("\n")}\n`);
  const bridge = new BridgeChat({ endpoint: "http://localhost:8091", token: "fixture-token", fetch: (async () => body) as typeof fetch });
  const seen: unknown[] = [];
  const reply = await bridge.stream({ content: "q" }, 1_000, (text) => seen.push(text), (queued) => seen.push(queued));
  assert.equal(reply.status, "ok");
  assert.deepEqual(seen, [true, false, "The ans"]);
});
