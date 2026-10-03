import test from "node:test";
import assert from "node:assert/strict";

import {
  PAGE_SIZE,
  pageRange,
  parseMemePage,
  parseMemeStatus,
  shortDate,
} from "../src/renderer/cards/memes_model.js";

test("a status is read, checked and defaulted", () => {
  assert.deepEqual(parseMemeStatus({ status: "ok", unsent: 30, sent: 8 }), { status: "ok", unsent: 30, sent: 8 });
  assert.deepEqual(parseMemeStatus({ unsent: 3 }), { status: "ok", unsent: 3, sent: 0 });
  assert.deepEqual(parseMemeStatus({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, undefined, "x", [], {}, { unsent: "many" }]) {
    assert.equal(parseMemeStatus(reply), undefined, JSON.stringify(reply));
  }
});

test("a page keeps its memes and drops what is not a meme", () => {
  const page = parseMemePage({
    total: 30,
    offset: 12,
    memes: [{ url: "https://images.example/a.png", title: "A", tags: "x", source: "s", createdAt: "2026-09-01T10:00:00", sentAt: "" }, "not a meme", null, { title: "no address" }],
  });

  assert.ok(page && !page.off);
  assert.equal(page.total, 30);
  assert.equal(page.offset, 12);
  assert.equal(page.memes.length, 2);
  assert.equal(page.memes[1]?.url, "");
  assert.deepEqual(parseMemePage({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, [], {}, { memes: "none" }]) {
    assert.equal(parseMemePage(reply), undefined, JSON.stringify(reply));
  }
});

test("a page says which memes it holds, counting from one", () => {
  const memes = Array.from({ length: PAGE_SIZE }, () => ({ url: "", title: "", tags: "", source: "", createdAt: "", sentAt: "" }));
  assert.deepEqual(pageRange({ total: 30, offset: 0, memes }), { from: 1, to: 12 });
  assert.deepEqual(pageRange({ total: 30, offset: 24, memes: memes.slice(0, 6) }), { from: 25, to: 30 });
  assert.deepEqual(pageRange({ total: 0, offset: 0, memes: [] }), { from: 0, to: 0 });
  assert.equal(PAGE_SIZE, 12);
});

test("a date is shown without its time", () => {
  assert.equal(shortDate("2026-09-03T10:00:00"), "2026-09-03");
  assert.equal(shortDate("2026-09-03"), "2026-09-03");
  assert.equal(shortDate(""), "");
  assert.equal(shortDate("yesterday"), "yesterday");
});
