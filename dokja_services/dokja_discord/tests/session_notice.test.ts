import assert from "node:assert/strict";
import test from "node:test";
import { localizeNotice } from "../messages/notices.js";

test("session start notices do not promise an inactivity reset", () => {
  assert.equal(localizeNotice("chat.session_started", "Hello", "en"), "New conversation session started.\n\nHello");
  assert.equal(localizeNotice("chat.session_started", "Olá", "pt-BR"), "Nova sessão de conversa iniciada.\n\nOlá");
  assert.equal(localizeNotice("chat.context_compacted", "Continuação", "pt-BR"), "O contexto da conversa foi compactado.\n\nContinuação");
});
