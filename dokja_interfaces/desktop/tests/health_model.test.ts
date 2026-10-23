import test from "node:test";
import assert from "node:assert/strict";

import { ACTIONS } from "../src/main/actions.js";
import {
  SERVICE_ORDER,
  formatInterval,
  intervalFields,
  intervalOf,
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
  assert.deepEqual(SERVICE_ORDER, ["meme", "chat", "book", "knowledge", "memory", "scheduler"]);
});

test("every status has a tone and the counts add up", () => {
  const health = parseHealth({
    services: {
      meme: entry("ok"),
      chat: entry("error", "refused"),
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
    health.rows.find((row) => row.name === "chat"),
    { name: "chat", status: "error", detail: "refused", tone: "problem" },
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
    data: { rows: [], interfaces: [], jobs: [], counts: { ok: 0, problem: 0, off: 0, unchecked: 0, other: 0, ...counts } },
  });

  assert.equal(pulseOf({ phase: "loading" }), "tuning");
  assert.equal(pulseOf({ phase: "error", error: { code: "unavailable", message: "" } }), "lost");
  assert.equal(pulseOf(ready({ ok: 4 })), "calm");
  assert.equal(pulseOf(ready({ ok: 3, problem: 1 })), "unwell");
  // not checked and unknown are not problems
  assert.equal(pulseOf(ready({ ok: 2, unchecked: 2, other: 1, off: 1 })), "calm");
});

test("a service has a switch only when the orchestrator says whether it is on", () => {
  const health = parseHealth({
    status: "ok",
    services: { meme: { status: "ok", enabled: false }, book: { status: "ok" }, memory: { status: "ok", enabled: "yes" } },
  });
  assert.deepEqual(health?.rows.map((row) => [row.name, row.enabled]), [["meme", false], ["book", undefined], ["memory", undefined]]);
});

test("jobs are read when they have a name and a switch, and there are none when the reply has none", () => {
  const health = parseHealth({
    status: "ok",
    services: {},
    jobs: [
      { name: "meme.refresh", enabled: true, interval: "6h0m0s", intervalOverride: true, lastAt: "2026-10-03T06:00:00Z", lastOutcome: "ran" },
      { name: "", enabled: true },
      { name: "no-flag" },
      "text",
      null,
    ],
  });
  assert.equal(health?.jobs.length, 1);
  assert.deepEqual(health?.jobs[0], {
    name: "meme.refresh", enabled: true, interval: "6h0m0s", intervalOverride: true,
    nextAt: "", lastAt: "2026-10-03T06:00:00Z", lastOutcome: "ran", lastError: "",
  });
  assert.deepEqual(parseHealth({ status: "ok", services: {} })?.jobs, []);
});

test("an interval reads as the person would say it", () => {
  assert.equal(formatInterval("6h0m0s"), "6h");
  assert.equal(formatInterval("45m0s"), "45m");
  assert.equal(formatInterval("1h30m0s"), "1h 30m");
  assert.equal(formatInterval("90s"), "1m 30s");
  assert.equal(formatInterval(""), "");
  assert.equal(formatInterval("soon"), "soon");
});

test("the editor starts from the interval in the unit that fits it", () => {
  assert.deepEqual(intervalFields("6h0m0s"), { amount: "6", unit: "h" });
  assert.deepEqual(intervalFields("45m0s"), { amount: "45", unit: "m" });
  assert.deepEqual(intervalFields("1h30m0s"), { amount: "90", unit: "m" });
  assert.deepEqual(intervalFields(""), { amount: "", unit: "m" });
  assert.deepEqual(intervalFields("soon"), { amount: "", unit: "m" });
});

test("the editor offers only an interval the orchestrator keeps", () => {
  assert.equal(intervalOf("45", "m"), "45m");
  assert.equal(intervalOf("6", "h"), "6h");
  assert.equal(intervalOf("007", "m"), "7m");
  assert.equal(intervalOf("720", "h"), "720h");
  for (const [amount, unit] of [["", "m"], ["0", "m"], ["0", "h"], ["721", "h"], ["43201", "m"], ["-5", "m"], ["1.5", "h"], ["abc", "m"], ["100000", "m"]] as const) {
    assert.equal(intervalOf(amount, unit), undefined, `${amount}${unit}`);
  }
});

test("whatever interval the editor offers, the shell lets it through", () => {
  const build = ACTIONS["scheduler.jobs.set"].payload;
  let offered = 0;
  for (const unit of ["m", "h"] as const) {
    for (const amount of ["1", "9", "10", "99", "100", "999", "1000", "9999", "10080", "43200", "43201", "720", "721", "99999", "100000"]) {
      const interval = intervalOf(amount, unit);
      if (interval === undefined) continue;
      offered += 1;
      assert.deepEqual(build({ name: "meme.refresh", interval }), { name: "meme.refresh", interval }, interval);
    }
  }
  assert.ok(offered >= 15, "the check really covered the editor's range");
});

test("interfaces are listed apart from the services, in the desktop's order, and counted with them", () => {
  const health = parseHealth({
    status: "degraded",
    services: {
      whatsapp: { status: "error", detail: "", kind: "interface", state: "logged_out", enabled: true },
      meme: entry("ok"),
      discord: { status: "disabled", detail: "switched off", kind: "interface", enabled: false },
      slack: { status: "ok", detail: "", kind: "interface", state: "connected" },
    },
  });

  assert.deepEqual(health?.rows.map((row) => row.name), ["meme"]);
  assert.deepEqual(health?.interfaces.map((row) => row.name), ["discord", "whatsapp", "slack"]);
  assert.equal(health?.interfaces[1]?.state, "logged_out");
  assert.equal(health?.interfaces[1]?.kind, "interface");
  assert.equal(health?.interfaces[0]?.state, undefined);
  assert.deepEqual(health?.counts, { ok: 2, problem: 1, off: 1, unchecked: 0, other: 0 });
});

test("a service whose name is an interface's is still a service unless it says it is an interface", () => {
  const health = parseHealth({ services: { whatsapp: entry("ok") } });

  assert.deepEqual(health?.rows.map((row) => row.name), ["whatsapp"]);
  assert.deepEqual(health?.interfaces, []);
});

test("an interface with a problem makes the name unwell", () => {
  const health = parseHealth({
    services: { whatsapp: { status: "error", detail: "", kind: "interface", state: "refused" } },
  }) as Health;

  assert.equal(pulseOf({ phase: "ready", data: health }), "unwell");
});

test("the scheduler notice names the enabled jobs and what they do", async () => {
  const { deliveryNotice } = await import("../src/renderer/cards/health.js");
  const t = ((id: string, values?: Record<string, string>) => `${id}:${values ? Object.values(values).join(",") : ""}`) as never;
  const health = { rows: [{ name: "scheduler", enabled: true }], interfaces: [{ name: "discord", enabled: false }], counts: {}, jobs: [
    { name: "meme.dispatch", enabled: true, interval: "2h0m0s" }, { name: "meme.refresh", enabled: false, interval: "45m0s" }] } as never;
  const lines = deliveryNotice("scheduler", health, t);
  assert.equal(lines.length, 1);
  // the job is named in words: the stand-in translator echoes the id of its name
  assert.match(lines[0] ?? "", /^delivery_job_posts:name_meme_dispatch:,2h$/);
  assert.deepEqual(deliveryNotice("discord", health, t), ["delivery_discord_voice:", "delivery_discord_switch_off:"]);
});
