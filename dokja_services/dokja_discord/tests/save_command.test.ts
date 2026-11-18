import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";

import { buildSaveReply, createSaveCommand } from "../commands/save_command.js";

test("a save is worded from what changed, in the interface locale", () => {
  assert.equal(buildSaveReply({ status: "ok", created: true }, "pt-BR"), "Guardei na sua biblioteca.");
  assert.equal(buildSaveReply({ status: "ok", created: false, changed: true }, "pt-BR"), "Já estava guardado; atualizei o que mudou.");
  assert.equal(buildSaveReply({ status: "ok", created: true }, "en"), "Saved to your library.");
});

test("refusals are worded from fixed codes and never show a link", () => {
  assert.equal(buildSaveReply({ status: "refused", refused: "not_owner" }, "pt-BR"), "Só o dono guarda documento na biblioteca.");
  assert.equal(buildSaveReply({ status: "refused", refused: "invalid_link" }, "en"), "That is not a link I can open.");
  assert.equal(buildSaveReply({ status: "refused", refused: "unavailable" }, "en"), "The library did not answer. Try again.");
  // An unknown code or a malformed result is a failure, not an invented save.
  assert.equal(buildSaveReply({ status: "refused", refused: "site said <b>no</b>" }, "en"), "I could not save it. Try again.");
  assert.equal(buildSaveReply({ status: "ok" }, "en"), "I could not save it. Try again.");
  assert.equal(buildSaveReply(undefined, "pt-BR"), "Não consegui guardar. Tente de novo.");
});

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

test("save_document sends the link as knowledge.save with the message identity and words the result", async () => {
  await withOrchestrator({ status: "ok", knowledge: { status: "ok", created: true } }, async (endpoint, sent) => {
    const replies: unknown[] = [];
    const command = createSaveCommand(endpoint, "pt-BR");
    await command.handle(context("https://dossie.example.com/a", replies) as never);
    assert.equal(replies[0], "Guardei na sua biblioteca.");
    const request = sent();
    assert.equal(request.event_type, "knowledge.save");
    assert.equal(request.payload.link, "https://dossie.example.com/a");
    assert.equal(request.payload.messageId, "m");
    assert.equal(request.payload.guildId, "g");
  });
});
