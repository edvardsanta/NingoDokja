import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";

import { buildQuizReply, createQuizCommand, QUIZ_TIMEOUT_MS } from "../commands/quiz_command.js";
import { FETCH_HEADERS_LIMIT_MS } from "../transport/bridge.js";

const finished = {
  status: "ok",
  url: "https://quiz.example.com/results?a=1",
  label: "Example verdict",
  answered: 246,
  comment: "Close, but the site measures a shadow.",
};

function context(link: string, replies: unknown[]) {
  return {
    userId: "u", channelId: "c", messageId: "m", guildId: "g",
    getString: (name: string) => (name === "link" ? link : ""),
    getNumber: () => undefined,
    reply: async (message: unknown) => { replies.push(message); },
    updateReply: async () => {},
    showModal: async () => {},
  };
}

test("a finished run shows the verdict, Ningo's comment and the site's own link", () => {
  assert.equal(
    buildQuizReply(finished, "pt-BR"),
    "Fiz o teste: 246 respostas. O site me chamou de **Example verdict**.\n\nClose, but the site measures a shadow.\n\nResultado: https://quiz.example.com/results?a=1",
  );
  assert.equal(
    buildQuizReply({ ...finished, label: "", comment: "" }, "en"),
    "I took the test: 246 answers.\n\nResult: https://quiz.example.com/results?a=1",
  );
});

test("refusals are worded from fixed codes and never show a link", () => {
  assert.equal(buildQuizReply({ status: "refused", refused: "busy" }, "pt-BR"), "Já estou fazendo um teste. Espere eu terminar.");
  assert.equal(buildQuizReply({ status: "refused", refused: "unanswered" }, "en"), "I could not answer every question, so I submitted nothing.");
  // An unknown code or a malformed result is a failure, not an invented result.
  assert.equal(buildQuizReply({ status: "refused", refused: "<b>site text</b>" }, "en"), "The test failed midway. I will not make up a result.");
  assert.equal(buildQuizReply({ status: "ok" }, "en"), "The test failed midway. I will not make up a result.");
  assert.equal(buildQuizReply(undefined, "pt-BR"), "O teste falhou no meio do caminho. Não vou inventar um resultado.");
});

// A quiz run waits longer than fetch allows, so it goes through node:http: these tests use a real
// local orchestrator instead of a fetch stub.
async function withOrchestrator(answer: object, run: (endpoint: string, sent: () => any) => Promise<void>) {
  let body = "";
  const server = createServer((request, response) => {
    let raw = "";
    request.on("data", (chunk) => { raw += chunk; });
    request.on("end", () => {
      body = raw;
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(answer));
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  try {
    await run(`http://127.0.0.1:${port}/orchestrator`, () => JSON.parse(body || "{}"));
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
}

test("political_test sends the link as quiz.take and replies publicly with the result", async () => {
  await withOrchestrator({ status: "ok", quiz: finished }, async (endpoint, sent) => {
    const replies: unknown[] = [];
    const command = createQuizCommand(endpoint, "pt-BR");
    assert.equal(command.name, "political_test");
    assert.equal(command.options?.[0].name, "link");
    assert.equal(command.options?.[0].required, true);
    await command.handle(context("https://quiz.example.com/start", replies));
    assert.equal(sent().event_type, "quiz.take");
    assert.equal(sent().payload.content, "https://quiz.example.com/start");
    assert.equal(sent().payload.locale, "pt-BR");
    assert.equal(replies.length, 1);
    assert.match(String(replies[0]), /Resultado: https:\/\/quiz\.example\.com\/results\?a=1$/);
  });
});

test("a switched-off orchestrator's reply and a transport failure reach the person", async () => {
  const replies: unknown[] = [];
  await withOrchestrator({ status: "ok", reply: "That is switched off right now (x)." }, async (endpoint) => {
    await createQuizCommand(endpoint, "en").handle(context("https://quiz.example.com", replies));
  });
  // Nothing listens on port 9 of the loopback: the request fails to connect.
  await createQuizCommand("http://127.0.0.1:9/orchestrator", "en").handle(context("https://quiz.example.com", replies));
  assert.deepEqual(replies, ["That is switched off right now (x).", "The test failed midway. I will not make up a result."]);
});

test("the command waits longer than the orchestrator's run but less than Discord's interaction", () => {
  assert.ok(QUIZ_TIMEOUT_MS > 13 * 60 * 1000 && QUIZ_TIMEOUT_MS < 15 * 60 * 1000);
  // Regression: fetch's five-minute headers limit failed every run that took longer.
  assert.ok(QUIZ_TIMEOUT_MS >= FETCH_HEADERS_LIMIT_MS);
});
