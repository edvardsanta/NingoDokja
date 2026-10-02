import test from "node:test";
import assert from "node:assert/strict";

import {
  MAX_SHOWN,
  SHOW_STEP,
  followingOf,
  nextStep,
  parseDigestPage,
  parseDigestStatus,
  sourceTone,
} from "../src/renderer/cards/digest_model.js";
import type { DigestStatus } from "../src/shared/replies.js";

const status = (overrides: Partial<DigestStatus> = {}): DigestStatus => ({
  configured: true, directoryError: "", ok: 0, failed: 0, pending: 0, disabled: 0, invalid: 0, items: 0, sources: [],
  ...overrides,
});
const source = (id: string, state: string) => ({ id, name: id, state, running: false, items: 0, skipped: 0, lastOk: "", error: "" });

test("a status is read, checked and defaulted", () => {
  const parsed = parseDigestStatus({
    configured: true, directoryError: "", ok: 2, failed: 1, pending: 0, disabled: 1, invalid: 0, items: 9,
    sources: [{ id: "a", name: "", state: "ok", items: 9, lastOk: "2026-10-02T10:00:00Z" }, { name: "no id" }, "x", null],
  });
  assert.deepEqual(parsed, {
    configured: true, directoryError: "", ok: 2, failed: 1, pending: 0, disabled: 1, invalid: 0, items: 9,
    sources: [{ id: "a", name: "a", state: "ok", running: false, items: 9, skipped: 0, lastOk: "2026-10-02T10:00:00Z", error: "" }],
  });
  assert.deepEqual(parseDigestStatus({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, undefined, "x", [], {}, { sources: "none" }]) {
    assert.equal(parseDigestStatus(reply), undefined, JSON.stringify(reply));
  }
});

test("a page keeps its items, drops what has no title and reads its counts", () => {
  const page = parseDigestPage({
    items: [{ id: "1", title: "One", summary: "S", source: "A", published: "2026-10-02T10:00:00Z" }, { id: "2", title: "  " }, 5],
    total: 20, offset: 0, more: 19, updated: "2026-10-02T10:30:00Z",
  });
  assert.deepEqual(page, {
    items: [{ id: "1", title: "One", summary: "S", source: "A", published: "2026-10-02T10:00:00Z" }],
    total: 20, offset: 0, more: 19, updated: "2026-10-02T10:30:00Z",
  });
  assert.deepEqual(parseDigestPage({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, [], {}, { items: "none" }]) {
    assert.equal(parseDigestPage(reply), undefined, JSON.stringify(reply));
  }
});

test("what the service is doing decides what the card explains", () => {
  assert.equal(followingOf(status({ directoryError: "cannot read" })), "directory-error");
  assert.equal(followingOf(status({ directoryError: "cannot read", configured: false })), "directory-error");
  assert.equal(followingOf(status({ configured: false })), "not-configured");
  assert.equal(followingOf(status()), "no-sources");
  assert.equal(followingOf(status({ sources: [source("a", "disabled"), source("b", "invalid")], disabled: 1, invalid: 1 })), "none-enabled");
  for (const counts of [{ ok: 1 }, { failed: 1 }, { pending: 1 }]) {
    assert.equal(followingOf(status({ sources: [source("a", "ok")], ...counts })), "following", JSON.stringify(counts));
  }
});

test("a source's state has a tone", () => {
  assert.deepEqual(["ok", "failed", "invalid", "pending", "disabled", "unknown", "anything"].map(sourceTone), [
    "ok", "problem", "problem", "unchecked", "off", "off", "off",
  ]);
});

test("the button offers one step, or what is left when that is less", () => {
  const page = (more: number) => ({ items: [], total: 0, offset: 0, more, updated: "" });
  assert.equal(nextStep(page(40)), SHOW_STEP);
  assert.equal(nextStep(page(SHOW_STEP)), SHOW_STEP);
  assert.equal(nextStep(page(3)), 3);
  assert.equal(nextStep(page(0)), 0);
  assert.ok(MAX_SHOWN >= SHOW_STEP && MAX_SHOWN % SHOW_STEP !== 1);
});
