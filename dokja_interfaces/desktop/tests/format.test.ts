import test from "node:test";
import assert from "node:assert/strict";

import { formatNumber } from "../src/renderer/i18n/format.js";

test("English writes a decimal point", () => {
  assert.equal(formatNumber("en", 0.081, 3), "0.081");
  assert.equal(formatNumber("en", 0.7, 2), "0.70");
  assert.equal(formatNumber("en", 12, 0), "12");
});

test("Portuguese writes a decimal comma", () => {
  assert.equal(formatNumber("pt", 0.081, 3), "0,081");
  assert.equal(formatNumber("pt", 0.7, 2), "0,70");
  assert.equal(formatNumber("pt", 12, 0), "12");
});

test("a signed number always shows its sign", () => {
  assert.equal(formatNumber("en", 0.7, 2, { signed: true }), "+0.70");
  assert.equal(formatNumber("pt", 0.7, 2, { signed: true }), "+0,70");
  assert.equal(formatNumber("en", -0.2, 2, { signed: true }), "-0.20");
  assert.equal(formatNumber("pt", -0.2, 2, { signed: true }), "-0,20");
});

test("the number of digits is kept, rounding the rest", () => {
  assert.equal(formatNumber("en", 0.0814, 3), "0.081");
  assert.equal(formatNumber("pt", 0.2, 3), "0,200");
});
