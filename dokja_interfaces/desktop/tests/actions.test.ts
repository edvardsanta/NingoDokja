import test from "node:test";
import assert from "node:assert/strict";

import { PROJECTIONS } from "../src/main/actions.js";

// Hand-built replies in the shapes the orchestrator uses, never a captured one: a real reply
// carries channel IDs and provider profiles.
const domainResult = {
  action: "inspect-ningo-platform",
  status: "degraded",
  services: {
    chat_ai: { status: "error", error: "connection refused" },
    meme: { status: "ok", unsent_count: 4, enabled: true },
    scheduler: { status: "stopped", detail: "never announced", enabled: false },
    knowledge: "not an object",
  },
  channels: { meme: ["channel-1"], safe_only: ["channel-1"] },
  chat_profiles: { profiles: [{ name: "profile-1", base_url: "base-url-1", key_hint: "hint-1" }] },
  jobs: [{ name: "job-1", last_error: "job-error-1" }],
};

const compact = { event_id: "event-1", workflow: "ningo", domain: "system", result: domainResult };
const verbose = {
  event: { event_id: "event-1" },
  workflow: "ningo",
  domains: ["system"],
  result: { system: domainResult },
};

const project = PROJECTIONS["ningo.status"];

test("ningo.status keeps only what the health card reads", () => {
  const projected = project(compact);

  assert.deepEqual(projected, {
    status: "degraded",
    services: {
      chat_ai: { status: "error", detail: "connection refused" },
      meme: { status: "ok", detail: "", enabled: true },
      scheduler: { status: "stopped", detail: "never announced", enabled: false },
    },
  });

  const serialized = JSON.stringify(projected);
  for (const leaked of ["channel-1", "base-url-1", "hint-1", "job-error-1", "unsent_count"]) {
    assert.ok(!serialized.includes(leaked), `${leaked} must not reach the renderer`);
  }
});

test("ningo.status reads the verbose layout, where the result is nested by domain", () => {
  assert.deepEqual(project(verbose), project(compact));
});

test("ningo.status without services still answers", () => {
  const bare = { workflow: "ningo", domain: "system", result: {} };
  assert.deepEqual(project(bare), { status: "ok", services: {} });
});

test("ningo.status refuses a reply that is not the system domain's", () => {
  const other = { workflow: "ningo", domain: "memory", result: { services: {} } };
  for (const reply of [null, undefined, "text", [], {}, { result: "text" }, other]) {
    assert.throws(() => project(reply), /unexpected status reply/);
  }
});

test("ningo.status cuts a long error text instead of passing it all on", () => {
  const long = "x".repeat(500);
  const reply = {
    workflow: "ningo",
    domain: "system",
    result: { services: { meme: { status: "error", error: long } } },
  };

  const projected = project(reply) as { services: { meme: { detail: string } } };
  assert.equal(projected.services.meme.detail.length, 200);
  assert.ok(projected.services.meme.detail.endsWith("…"));
});
