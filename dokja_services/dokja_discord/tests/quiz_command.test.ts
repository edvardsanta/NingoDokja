import test from "node:test";
import assert from "node:assert/strict";

import { buildQuizReply, createQuizCommand, QUIZ_TIMEOUT_MS } from "../commands/quiz_command.js";

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

test("teste_politico sends the link as quiz.take and replies publicly with the result", async () => {
  const originalFetch = globalThis.fetch;
  let sent: any;
  globalThis.fetch = (async (_input, init) => {
    sent = JSON.parse(String(init?.body ?? "{}"));
    return new Response(JSON.stringify({ status: "ok", quiz: finished }), {
      status: 200, headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;
  try {
    const replies: unknown[] = [];
    const command = createQuizCommand("http://orch", "pt-BR");
    assert.equal(command.name, "teste_politico");
    assert.equal(command.options?.[0].name, "link");
    assert.equal(command.options?.[0].required, true);
    await command.handle(context("https://quiz.example.com/start", replies));
    assert.equal(sent.event_type, "quiz.take");
    assert.equal(sent.payload.content, "https://quiz.example.com/start");
    assert.equal(sent.payload.locale, "pt-BR");
    assert.equal(replies.length, 1);
    assert.match(String(replies[0]), /Resultado: https:\/\/quiz\.example\.com\/results\?a=1$/);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("a switched-off orchestrator's reply and a transport failure reach the person", async () => {
  const originalFetch = globalThis.fetch;
  try {
    globalThis.fetch = (async () => new Response(JSON.stringify({ status: "ok", reply: "That is switched off right now (x)." }), {
      status: 200, headers: { "content-type": "application/json" },
    })) as typeof fetch;
    const replies: unknown[] = [];
    await createQuizCommand("http://orch", "en").handle(context("https://quiz.example.com", replies));
    globalThis.fetch = (async () => { throw new Error("network down"); }) as typeof fetch;
    await createQuizCommand("http://orch", "en").handle(context("https://quiz.example.com", replies));
    assert.deepEqual(replies, ["That is switched off right now (x).", "The test failed midway. I will not make up a result."]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("the command waits longer than the orchestrator's run but less than Discord's interaction", () => {
  assert.ok(QUIZ_TIMEOUT_MS > 13 * 60 * 1000 && QUIZ_TIMEOUT_MS < 15 * 60 * 1000);
});
