import test from "node:test";
import assert from "node:assert/strict";

import { PreviewCache } from "../src/renderer/cards/preview_cache.js";

test("a preview is remembered and given back", () => {
  const cache = new PreviewCache(3, 1000);
  cache.remember("a", "data-a");
  assert.equal(cache.get("a"), "data-a");
  assert.equal(cache.get("b"), undefined);
  cache.clear();
  assert.equal(cache.get("a"), undefined);
});

test("the oldest preview goes first when there are too many", () => {
  const cache = new PreviewCache(2, 1000);
  cache.remember("a", "1");
  cache.remember("b", "2");
  cache.remember("c", "3");
  assert.deepEqual(["a", "b", "c"].map((url) => cache.get(url)), [undefined, "2", "3"]);
});

test("a preview asked for again is not the oldest any more", () => {
  const cache = new PreviewCache(2, 1000);
  cache.remember("a", "1");
  cache.remember("b", "2");
  cache.remember("a", "1");
  cache.remember("c", "3");
  assert.deepEqual(["a", "b", "c"].map((url) => cache.get(url)), ["1", undefined, "3"]);
});

test("the oldest go first when the data is too big, however few they are", () => {
  const cache = new PreviewCache(10, 100);
  cache.remember("a", "x".repeat(60));
  cache.remember("b", "y".repeat(30));
  cache.remember("c", "z".repeat(30));
  assert.deepEqual(["a", "b", "c"].map((url) => cache.get(url)?.length), [undefined, 30, 30]);
});

test("the size is counted again when an address is replaced", () => {
  const cache = new PreviewCache(10, 100);
  cache.remember("a", "x".repeat(90));
  cache.remember("a", "x".repeat(10));
  cache.remember("b", "y".repeat(90));
  assert.deepEqual(["a", "b"].map((url) => cache.get(url)?.length), [10, 90]);
});

test("a file bigger than the whole budget is not kept, and keeps nothing else out", () => {
  const cache = new PreviewCache(10, 100);
  cache.remember("a", "x".repeat(50));
  cache.remember("huge", "h".repeat(101));
  assert.equal(cache.get("huge"), undefined);
  assert.equal(cache.get("a")?.length, 50);
});
