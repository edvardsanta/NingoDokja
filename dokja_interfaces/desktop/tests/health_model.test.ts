import test from "node:test";
import assert from "node:assert/strict";

import {
  SERVICE_ORDER,
  parseHealth,
  pulseOf,
  toneOf,
  type Health,
} from "../src/renderer/cards/health_model.js";

const entry = (status: string, detail = "") => ({ status, detail });

test("services come in the TUI's order, then any other by name", () => {
  const health = parseHealth({
    status: "ok",
    services: {
      zeta: entry("ok"),
      scheduler: entry("ok"),
      alpha: entry("ok"),
      meme: entry("ok"),
      knowledge: entry("ok"),
    },
  });

  assert.deepEqual(
    health?.rows.map((row) => row.name),
    ["meme", "knowledge", "scheduler", "alpha", "zeta"],
  );
  assert.deepEqual(SERVICE_ORDER, ["meme", "chat_ai", "book", "knowledge", "memory", "scheduler"]);
});

test("every status has a tone and the counts add up", () => {
  const health = parseHealth({
    services: {
      meme: entry("ok"),
      chat_ai: entry("error", "refused"),
      book: entry("unchecked"),
      knowledge: entry("degraded"),
      memory: entry("disabled"),
      scheduler: entry("stopped"),
      extra: entry("something new"),
    },
  });

  assert.ok(health);
  assert.deepEqual(health.counts, { ok: 1, problem: 3, off: 1, unchecked: 1, other: 1 });
  assert.equal(health.rows.length, 7);
  assert.deepEqual(
    health.rows.find((row) => row.name === "chat_ai"),
    { name: "chat_ai", status: "error", detail: "refused", tone: "problem" },
  );
  // a status the screen does not know is kept as it came
  assert.equal(health.rows.find((row) => row.name === "extra")?.status, "something new");
});

test("toneOf maps the orchestrator's statuses", () => {
  assert.equal(toneOf("ok"), "ok");
  assert.equal(toneOf("disabled"), "off");
  for (const problem of ["error", "stopped", "degraded"]) assert.equal(toneOf(problem), "problem");
  assert.equal(toneOf("unchecked"), "unchecked");
  assert.equal(toneOf(""), "other");
  assert.equal(toneOf("OK"), "other");
});

test("a reply without services, or with odd entries, is handled", () => {
  for (const reply of [null, undefined, "text", 3, [], {}, { services: "no" }, { services: [] }]) {
    assert.equal(parseHealth(reply), undefined, JSON.stringify(reply));
  }

  const health = parseHealth({ services: { meme: entry("ok"), broken: "not an entry", gone: null } });
  assert.deepEqual(
    health?.rows.map((row) => row.name),
    ["meme"],
  );
  assert.deepEqual(parseHealth({ services: {} })?.rows, []);
});

test("the name's pulse follows what the card knows", () => {
  const ready = (counts: Partial<Health["counts"]>): Parameters<typeof pulseOf>[0] => ({
    phase: "ready",
    data: { rows: [], counts: { ok: 0, problem: 0, off: 0, unchecked: 0, other: 0, ...counts } },
  });

  assert.equal(pulseOf({ phase: "loading" }), "tuning");
  assert.equal(pulseOf({ phase: "error", error: { code: "unavailable", message: "" } }), "lost");
  assert.equal(pulseOf(ready({ ok: 4 })), "calm");
  assert.equal(pulseOf(ready({ ok: 3, problem: 1 })), "unwell");
  // not checked and unknown are not problems
  assert.equal(pulseOf(ready({ ok: 2, unchecked: 2, other: 1, off: 1 })), "calm");
});
