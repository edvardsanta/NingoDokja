import assert from "node:assert/strict";
import test from "node:test";

import { SendLimit, sendLimits } from "../runtime/send_limit.js";

test("the fixed budget is a few messages a minute and a few dozen an hour", () => {
  assert.deepEqual(sendLimits, [
    { count: 6, windowMs: 60_000 },
    { count: 40, windowMs: 3_600_000 },
  ]);
});

test("the seventh message in a minute is refused, and takes nothing", () => {
  let now = 0;
  const limit = new SendLimit(undefined, () => now);

  for (let n = 0; n < 6; n += 1) {
    assert.equal(limit.take(), true, `message ${n + 1}`);
  }
  assert.equal(limit.take(), false);
  assert.equal(limit.take(), false);

  now = 60_001;
  assert.equal(limit.take(), true, "a minute later there is room again");
});

test("the hour is capped even when every minute is polite", () => {
  let now = 0;
  const limit = new SendLimit(undefined, () => now);
  let sent = 0;
  for (let minute = 0; minute < 60; minute += 1) {
    now = minute * 60_000 + 1;
    for (let n = 0; n < 6; n += 1) {
      if (limit.take()) sent += 1;
    }
  }

  assert.equal(sent, 40);
});

test("a custom budget is respected", () => {
  let now = 0;
  const limit = new SendLimit([{ count: 2, windowMs: 1000 }], () => now);

  assert.deepEqual([limit.take(), limit.take(), limit.take()], [true, true, false]);
  now = 1001;
  assert.equal(limit.take(), true);
});
