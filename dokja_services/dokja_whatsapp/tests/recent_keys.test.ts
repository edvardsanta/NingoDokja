import assert from "node:assert/strict";
import test from "node:test";

import { RecentKeys } from "../messages/recent_keys.js";

test("a key is new once and known afterwards", () => {
  const keys = new RecentKeys();

  assert.equal(keys.seen("a"), false);
  assert.equal(keys.seen("a"), true);
  assert.equal(keys.seen("b"), false);
});

test("add and has record and look up without side effects", () => {
  const keys = new RecentKeys();

  assert.equal(keys.has("a"), false);
  assert.equal(keys.has("a"), false);
  keys.add("a");
  assert.equal(keys.has("a"), true);
});

test("a key is forgotten after the time to live", () => {
  let now = 0;
  const keys = new RecentKeys(1000, 100, () => now);

  keys.seen("a");
  now = 1001;

  assert.equal(keys.has("a"), false);
  assert.equal(keys.seen("a"), false);
});

test("the oldest keys are dropped past the size limit", () => {
  const keys = new RecentKeys(60_000, 2);

  keys.seen("a");
  keys.seen("b");
  keys.seen("c");

  assert.equal(keys.has("a"), false);
  assert.equal(keys.has("b"), true);
  assert.equal(keys.has("c"), true);
});
