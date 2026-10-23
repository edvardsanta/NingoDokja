import assert from "node:assert/strict";
import test from "node:test";

import { SentMessages } from "../runtime/sent_messages.js";

test("a stored message is found and an unknown one is not", () => {
  const sent = new SentMessages<string>();

  sent.set("a", "first");

  assert.equal(sent.get("a"), "first");
  assert.equal(sent.get("b"), undefined);
});

test("a message is forgotten after the time to live", () => {
  let now = 0;
  const sent = new SentMessages<string>(1000, 10, () => now);

  sent.set("a", "first");
  now = 1000;
  assert.equal(sent.get("a"), "first");
  now = 1001;

  assert.equal(sent.get("a"), undefined);
});

test("the oldest messages are dropped past the size limit", () => {
  const sent = new SentMessages<string>(60_000, 2);

  sent.set("a", "1");
  sent.set("b", "2");
  sent.set("c", "3");

  assert.equal(sent.get("a"), undefined);
  assert.equal(sent.get("b"), "2");
  assert.equal(sent.get("c"), "3");
});

test("storing an id again replaces the content and renews its age", () => {
  let now = 0;
  const sent = new SentMessages<string>(1000, 10, () => now);

  sent.set("a", "old");
  now = 900;
  sent.set("a", "new");
  now = 1500;

  assert.equal(sent.get("a"), "new");
});
