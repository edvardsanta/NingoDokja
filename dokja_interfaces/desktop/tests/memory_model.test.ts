import test from "node:test";
import assert from "node:assert/strict";

import {
  parseMemoryScore,
  parseMemoryStatus,
  scoreViewOf,
  similarityOf,
} from "../src/renderer/cards/memory_model.js";

const status = {
  experiences: 40,
  pending: 12,
  resolved: 25,
  expired: 3,
  embedded: 38,
  needsReindex: 2,
  embedModel: "model-1",
  embeddings: true,
  embedderReachable: true,
};

const score = {
  scored: 31,
  unscored: 4,
  minScored: 30,
  brierPrediction: 0.081,
  brierBaseline: 0.27,
  skill: 0.7,
  beatsBaseline: true,
  enoughData: true,
};

test("a status is read, checked and defaulted", () => {
  assert.deepEqual(parseMemoryStatus(status), status);
  assert.deepEqual(parseMemoryStatus({ off: true, reason: "switched off" }), { off: true, reason: "switched off" });
  assert.equal(parseMemoryStatus({ experiences: 1 })?.off, undefined);
  for (const reply of [null, undefined, "text", [], {}, { experiences: "many" }]) {
    assert.equal(parseMemoryStatus(reply), undefined, JSON.stringify(reply));
  }
});

test("a score is read, checked and defaulted", () => {
  assert.deepEqual(parseMemoryScore(score), score);
  assert.deepEqual(parseMemoryScore({ off: true }), { off: true, reason: "" });
  for (const reply of [null, [], {}, { scored: "many" }]) {
    assert.equal(parseMemoryScore(reply), undefined, JSON.stringify(reply));
  }
});

test("similarity is off, down or on", () => {
  const base = parseMemoryStatus(status);
  assert.ok(base && !base.off);
  assert.equal(similarityOf(base), "on");
  assert.equal(similarityOf({ ...base, embedderReachable: false }), "down");
  assert.equal(similarityOf({ ...base, embeddings: false, embedderReachable: false }), "off");
});

test("the score says nothing, too few or a verdict, and a failure is its own thing", () => {
  assert.deepEqual(scoreViewOf({ ok: false, error: { code: "timeout", message: "slow" } }), {
    kind: "error",
    message: "slow",
  });
  assert.deepEqual(scoreViewOf({ ok: true, result: { nonsense: true } }), { kind: "error", message: "unexpected reply" });
  assert.deepEqual(scoreViewOf({ ok: true, result: { off: true, reason: "off" } }), { kind: "off", reason: "off" });
  assert.deepEqual(scoreViewOf({ ok: true, result: { ...score, scored: 0, enoughData: false } }), { kind: "nothing" });
  assert.equal(scoreViewOf({ ok: true, result: { ...score, scored: 12, enoughData: false } }).kind, "too-few");
  assert.equal(scoreViewOf({ ok: true, result: score }).kind, "judged");
});
