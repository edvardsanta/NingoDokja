import test from "node:test";
import assert from "node:assert/strict";

import { formatAgo, formatNumber } from "../src/renderer/i18n/format.js";

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

const NOW = Date.parse("2026-10-02T12:00:00Z");
const before = (seconds: number) => new Date(NOW - seconds * 1000).toISOString();

test("a past time is said in the language of the screen", () => {
  assert.equal(formatAgo("en", before(2 * 3600 + 600), NOW), "2 hours ago");
  assert.equal(formatAgo("pt", before(2 * 3600 + 600), NOW), "há 2 horas");
  assert.equal(formatAgo("en", before(5 * 60 + 20), NOW), "5 minutes ago");
  assert.equal(formatAgo("pt", before(5 * 60 + 20), NOW), "há 5 minutos");
  assert.equal(formatAgo("en", before(3 * 86400 + 3600), NOW), "3 days ago");
  assert.equal(formatAgo("pt", before(86400 + 3600), NOW), "ontem");
  assert.equal(formatAgo("en", before(86400 + 3600), NOW), "yesterday");
});

test("less than a minute ago, and a time ahead of the clock, read as now", () => {
  assert.equal(formatAgo("en", before(20), NOW), "now");
  assert.equal(formatAgo("pt", before(20), NOW), "agora");
  assert.equal(formatAgo("en", before(-1800), NOW), "now");
});

test("the largest whole unit is used, never rounded up", () => {
  assert.equal(formatAgo("en", before(59 * 60 + 59), NOW), "59 minutes ago");
  assert.equal(formatAgo("en", before(3600), NOW), "1 hour ago");
  assert.equal(formatAgo("en", before(23 * 3600 + 3599), NOW), "23 hours ago");
});

test("text that is not a time says nothing", () => {
  for (const text of ["", "yesterday", "2026-13-45"]) assert.equal(formatAgo("en", text, NOW), "", text);
});
