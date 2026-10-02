import test from "node:test";
import assert from "node:assert/strict";

import {
  parseKnowledgeSearch,
  parseKnowledgeStatus,
  similarityOf,
} from "../src/renderer/cards/knowledge_model.js";

const status = {
  documents: 6,
  chunks: 52,
  embedded: 50,
  pendingEmbeddings: 2,
  embedModel: "model-1",
  embedderReachable: true,
};

test("a status is read, checked and defaulted", () => {
  assert.deepEqual(parseKnowledgeStatus(status), status);
  assert.deepEqual(parseKnowledgeStatus({ documents: 1 }), {
    documents: 1,
    chunks: 0,
    embedded: 0,
    pendingEmbeddings: 0,
    embedModel: "",
    embedderReachable: null,
  });
  assert.deepEqual(parseKnowledgeStatus({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, undefined, "x", [], {}, { documents: "six" }]) {
    assert.equal(parseKnowledgeStatus(reply), undefined, JSON.stringify(reply));
  }
});

test("similarity is on, down, or off when the service does not say", () => {
  const base = parseKnowledgeStatus(status);
  assert.ok(base && !base.off);
  assert.equal(similarityOf(base), "on");
  assert.equal(similarityOf({ ...base, embedderReachable: false }), "down");
  assert.equal(similarityOf({ ...base, embedderReachable: null }), "off");
});

test("a search keeps its hits and drops what is not a hit", () => {
  const parsed = parseKnowledgeSearch({
    query: "free will",
    hits: [
      { rank: 1, title: "One", text: "t", score: 0.5, relevant: true, tags: ["a", 3, ""] },
      "not a hit",
      { rank: 2, title: "Two", relevant: "yes", score: "high" },
    ],
    relevantCount: 1,
    threshold: 0.45,
    degraded: true,
    reason: "similarity is down",
  });

  assert.ok(parsed && !parsed.off);
  assert.equal(parsed.hits.length, 2);
  assert.deepEqual(parsed.hits[0]?.tags, ["a"]);
  assert.equal(parsed.hits[1]?.relevant, false, "relevant only when exactly true");
  assert.equal(parsed.hits[1]?.score, null);
  assert.equal(parsed.degraded, true);
  assert.deepEqual(parseKnowledgeSearch({ off: true, reason: "paused" }), { off: true, reason: "paused" });
  for (const reply of [null, [], {}, { hits: "none" }]) {
    assert.equal(parseKnowledgeSearch(reply), undefined, JSON.stringify(reply));
  }
});
