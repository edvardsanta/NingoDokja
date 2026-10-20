import test from "node:test";
import assert from "node:assert/strict";

import { parseMemePage } from "../src/renderer/cards/memes_model.js";

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
