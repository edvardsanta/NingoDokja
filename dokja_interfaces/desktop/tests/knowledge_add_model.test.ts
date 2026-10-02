import test from "node:test";
import assert from "node:assert/strict";

import { check, outcomeOf, parseIngestResult, type Draft } from "../src/renderer/cards/knowledge_add_model.js";
import { INGEST_LIMITS } from "../src/shared/ingest.js";

const draft = (overrides: Partial<Draft> = {}): Draft => ({ mode: "note", title: "", body: "", address: "", tags: "", ...overrides });

test("a note needs a title and a text; both are sent trimmed where it matters", () => {
  assert.deepEqual(check(draft({ title: "  A thought ", body: "Keep this.\n", tags: "ethics, kant , ethics" })), {
    ok: true,
    payload: { mode: "note", title: "A thought", body: "Keep this.\n", tags: ["ethics", "kant"] },
  });
  assert.deepEqual(check(draft({ body: "text" })), { ok: false, problem: { id: "add_need_title" } });
  assert.deepEqual(check(draft({ title: "T", body: "  \n " })), { ok: false, problem: { id: "add_need_text" } });
  assert.deepEqual(check(draft({ title: "T", body: "x".repeat(INGEST_LIMITS.noteChars + 1) })), {
    ok: false,
    problem: { id: "add_text_too_long", params: { max: INGEST_LIMITS.noteChars } },
  });
  assert.deepEqual(check(draft({ title: "x".repeat(INGEST_LIMITS.titleChars + 1), body: "text" })), {
    ok: false,
    problem: { id: "add_title_too_long", params: { max: INGEST_LIMITS.titleChars } },
  });
});

test("an address must be a web address, and its title is optional", () => {
  assert.deepEqual(check(draft({ mode: "address", address: " https://example.com/a " })), {
    ok: true,
    payload: { mode: "address", address: "https://example.com/a" },
  });
  assert.deepEqual(check(draft({ mode: "address", address: "http://example.com", title: "A page" })), {
    ok: true,
    payload: { mode: "address", title: "A page", address: "http://example.com" },
  });
  assert.deepEqual(check(draft({ mode: "address" })), { ok: false, problem: { id: "add_need_address" } });
  for (const address of ["example.com", "plugin:thing", "file:///etc/passwd", "ftp://example.com", "javascript:alert(1)", "https://", "text with spaces"]) {
    assert.deepEqual(check(draft({ mode: "address", address })), { ok: false, problem: { id: "add_bad_address" } }, address);
  }
});

test("a file needs a file already read", () => {
  assert.deepEqual(check(draft({ mode: "file" })), { ok: false, problem: { id: "add_need_file" } });
  assert.deepEqual(check(draft({ mode: "file", file: { filename: "a.txt", content: "QUJD", bytes: 3 }, title: "A file" })), {
    ok: true,
    payload: { mode: "file", title: "A file", filename: "a.txt", content: "QUJD" },
  });
});

test("tags are a few short words", () => {
  const many = Array.from({ length: INGEST_LIMITS.tags + 1 }, (_, i) => `t${i}`).join(",");
  assert.deepEqual(check(draft({ title: "T", body: "b", tags: many })), {
    ok: false,
    problem: { id: "add_too_many_tags", params: { max: INGEST_LIMITS.tags } },
  });
  assert.deepEqual(check(draft({ title: "T", body: "b", tags: "x".repeat(INGEST_LIMITS.tagChars + 1) })), {
    ok: false,
    problem: { id: "add_tag_too_long", params: { max: INGEST_LIMITS.tagChars } },
  });
  assert.deepEqual(check(draft({ title: "T", body: "b", tags: " , ," })).ok, true);
});

test("the reply is read, checked and defaulted", () => {
  assert.deepEqual(parseIngestResult({ count: 2, created: 1, updated: 0, unchanged: 1, chunks: 4, degraded: true, reason: "no embedder" }), {
    count: 2, created: 1, updated: 0, unchanged: 1, chunks: 4, degraded: true, reason: "no embedder",
  });
  assert.deepEqual(parseIngestResult({ count: 1 }), { count: 1, created: 0, updated: 0, unchanged: 0, chunks: 0, degraded: false, reason: "" });
  assert.deepEqual(parseIngestResult({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, undefined, "x", [], {}, { count: "1" }]) assert.equal(parseIngestResult(reply), undefined, JSON.stringify(reply));
});

test("what is said depends on what happened", () => {
  const result = { count: 1, created: 0, updated: 0, unchanged: 0, chunks: 0, degraded: false, reason: "" };
  assert.deepEqual(outcomeOf({ ...result, created: 1, chunks: 3 }), { id: "add_created", params: { chunks: 3 } });
  assert.deepEqual(outcomeOf({ ...result, updated: 1, chunks: 2 }), { id: "add_updated", params: { chunks: 2 } });
  assert.deepEqual(outcomeOf({ ...result, unchanged: 1 }), { id: "add_unchanged" });
  assert.deepEqual(outcomeOf({ ...result, count: 5, created: 2, updated: 1, unchanged: 2 }), {
    id: "add_many",
    params: { count: 5, created: 2, updated: 1, unchanged: 2 },
  });
  assert.deepEqual(outcomeOf({ ...result, count: 0 }), { id: "add_nothing" });
});
