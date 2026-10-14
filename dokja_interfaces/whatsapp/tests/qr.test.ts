import assert from "node:assert/strict";
import test from "node:test";

import { qrRows, renderQr } from "../runtime/qr.js";

test("the rows are a square of 0 and 1 with no markup", () => {
  const rows = qrRows("2@ref,noise,identity,adv");

  assert.ok(rows.length >= 21 && rows.length <= 177);
  assert.ok(rows.every((row) => row.length === rows.length && /^[01]+$/.test(row)));
});

test("the same text always gives the same rows, and another text another", () => {
  assert.deepEqual(qrRows("one"), qrRows("one"));
  assert.notDeepEqual(qrRows("one"), qrRows("two"));
});

test("the terminal drawing is printed once and holds no text of the code", () => {
  const printed: string[] = [];

  renderQr("2@secret-code", (text) => printed.push(text));

  assert.equal(printed.length, 1);
  assert.ok(printed[0].split("\n").length > 10);
  assert.ok(!printed[0].includes("secret-code"));
});
