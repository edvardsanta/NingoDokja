import test from "node:test";
import assert from "node:assert/strict";

import { cheapestOf, leadOf, parsePriceSearch, signalsOf, sparkOf, updatedOf, verdictCounts, verdictOf } from "../src/renderer/cards/prices_model.js";
import type { PriceProduct, PriceRow } from "../src/shared/replies.js";

const row = (overrides: Partial<PriceRow>): PriceRow => ({
  merchant: "store-a", series: "merchant", kind: "pix", verdict: "no_discount", signals: [],
  latestOn: "2026-11-08", latestCents: 1000, median30Cents: 0, lowestCents: 0, ...overrides,
});
const product = (rows: PriceRow[], market: PriceProduct["market"] = []): PriceProduct => ({ key: "p", title: "P", rows, market });

test("a warning speaks for a product before a discount, and an unknown code last", () => {
  assert.equal(leadOf(product([row({ verdict: "real_discount" }), row({ verdict: "inflated_before_sale" })])).code, "inflated_before_sale");
  assert.equal(leadOf(product([row({ verdict: "insufficient_history" }), row({ verdict: "real_discount" })])).code, "real_discount");
  assert.equal(verdictOf("made_up").label, "price_verdict_unknown");
  assert.equal(leadOf(product([])).label, "price_verdict_unknown");
});

test("signals are named once, the cheapest row and the newest day are found", () => {
  const p = product([
    row({ signals: ["all_time_low", "unknown_signal"], latestCents: 900, latestOn: "2026-11-07" }),
    row({ signals: ["all_time_low", "raised_before_drop"], latestCents: 800, latestOn: "2026-11-08", kind: "card" }),
  ]);
  assert.deepEqual(signalsOf(p), ["price_signal_all_time_low", "price_signal_raised_before_drop"]);
  assert.equal(cheapestOf(p)?.kind, "card");
  assert.equal(updatedOf(p), "2026-11-08");
});

test("products are counted by their leading verdict", () => {
  const counts = verdictCounts([product([row({ verdict: "real_discount" })]), product([row({ verdict: "real_discount" })]), product([row({})])]);
  assert.equal(counts.get("real_discount"), 2);
  assert.equal(counts.get("no_discount"), 1);
});

test("the market line spans the box, oldest left, and one day draws nothing", () => {
  assert.equal(sparkOf([{ on: "2026-11-07", cents: 5 }]), undefined);
  const spark = sparkOf([{ on: "a", cents: 100 }, { on: "b", cents: 300 }, { on: "c", cents: 200 }], 100, 20, 0);
  assert.deepEqual(spark, { points: "0,20 50,0 100,10", lastX: 100, lastY: 10, min: 100, max: 300 });
  // a flat line stays inside the box
  assert.equal(sparkOf([{ on: "a", cents: 5 }, { on: "b", cents: 5 }], 10, 10, 1)?.points, "0,9 10,9");
});

test("a reply is checked: off passes, a broken one is refused, bad points are dropped", () => {
  assert.deepEqual(parsePriceSearch({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, "x", [], {}, { products: "none" }]) assert.equal(parsePriceSearch(reply), undefined, JSON.stringify(reply));
  const parsed = parsePriceSearch({ matched: 1, products: [{ key: "k", title: "T", rows: [{ merchant: "m", latestCents: 5, kind: "pix" }], market: [{ on: "2026-11-07", cents: 3 }, { on: "x", cents: 3 }] }, { title: "no key" }] });
  assert.ok(parsed && !parsed.off);
  assert.equal(parsed.products.length, 1);
  assert.equal(parsed.products[0]?.market.length, 1);
  assert.equal(parsed.products[0]?.rows[0]?.kind, "pix");
});
