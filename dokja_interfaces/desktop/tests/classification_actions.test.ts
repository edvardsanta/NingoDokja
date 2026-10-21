import test from "node:test";
import assert from "node:assert/strict";
import { ACTIONS } from "../src/main/actions.js";
import { handleRequest } from "../src/main/ipc.js";

const revision = "a".repeat(64);
const limits = { defaultTimeoutMs: 15_000, maxTimeoutMs: 60_000 };
const body = {
  source_id: "study", revision,
  metadata: { form: "book", form_choice: { origin: "classifier", classifier_id: "reader", classifier_version: "2" }, content_kind: null, extra: "private" },
  classification: { needs_user_input: true, questions: [{ field: "content_kind", question: "Choose the content kind", allowed_values: ["research", "reference"] }] },
  text: "private document text", internal: { private: "discard" },
};

test("classification questions are read-only; answers require presence and a revision", async () => {
  const calls: unknown[] = [];
  const client = { async request(type: string, payload: Record<string, unknown>) {
    calls.push({ type, payload });
    return { status: "ok", result: { domain: "knowledge", result: body } };
  } };
  const query = await handleRequest({ type: "knowledge.classify", payload: { source_id: "study", extra: "discard" } }, client, limits);
  assert.equal(query.ok, true);
  if (!query.ok) throw new Error("query failed");
  const projected = query.result as Record<string, unknown>;
  assert.equal(projected.sourceId, "study");
  assert.equal("text" in projected, false);
  assert.equal("metadata" in projected, false);
  assert.deepEqual(projected.form, { value: "book", origin: "classifier", classifierId: "reader", classifierVersion: "2", reason: "" });
  const submit = { type: "knowledge.classify", payload: { source_id: "study", revision, answers: { content_kind: "research" } } };
  const denied = await handleRequest(submit, client, limits);
  assert.ok(!denied.ok && denied.error.code === "denied");
  assert.equal(calls.length, 1);
  const allowed = await handleRequest(submit, client, limits, undefined, { recent: () => true });
  assert.equal(allowed.ok, true);
  assert.deepEqual(calls, [
    { type: "knowledge.classify", payload: { source_id: "study" } },
    { type: "knowledge.classify", payload: submit.payload },
  ]);
});

test("classification input refuses empty, stale-shaped or injected answers", () => {
  const build = ACTIONS["knowledge.classify"].payload;
  for (const raw of [
    {}, { source_id: "study", revision },
    { source_id: "study", answers: { form: "book" } },
    { source_id: "study", revision, answers: {} },
    { source_id: "study", revision, answers: { unknown: "book" } },
    { source_id: "study", revision, answers: { form: "" } },
    { source_id: "study", revision, answers: { form: null } },
    { source_id: "study", revision, answers: { form: "<script>" } },
  ]) assert.equal(build(raw), undefined, JSON.stringify(raw));
  // The domain owns the vocabulary; a future valid label can pass without a UI release.
  assert.deepEqual(build({ source_id: "study", revision, answers: { form: "future_form" }, body: "discard" }), { source_id: "study", revision, answers: { form: "future_form" } });
});

test("classification projection rejects malformed questions and handles disabled services", () => {
  const project = (result: unknown) => ACTIONS["knowledge.classify"].project({ domain: "knowledge", result });
  assert.throws(() => project({ ...body, revision: "invalid" }));
  assert.throws(() => project({ ...body, classification: { needs_user_input: true, questions: [] } }));
  assert.throws(() => project({ ...body, classification: { needs_user_input: true, questions: [{ field: "anything", allowed_values: ["book"] }] } }));
  assert.deepEqual(project({ skipped: true, reason: "paused" }), { off: true, reason: "paused" });
});
