import assert from "node:assert/strict";
import test from "node:test";

import { answersNobody, changesOf, checkAlias, checkChannel, checkGroup, checkPhone, checkTarget, parseList, problemsOf, refusedSetting, sectionsOf, valuesOf } from "../src/renderer/cards/settings_model.js";
import type { RuntimeSetting } from "../src/shared/runtime.js";

const row = (id: string, value: string, extra: Partial<RuntimeSetting> = {}): RuntimeSetting =>
  ({ id, group: id.split(".")[0]!, kind: "list", options: [], value, affects: ["whatsapp"], note: "", default: "", ...extra });

test("each entry is checked with the rules the controller applies", () => {
  assert.equal(checkChannel(" 123 ").ok, true);
  assert.equal(checkChannel("12a").ok, false);
  assert.deepEqual(checkPhone("+55 (11) 91234-5678"), { ok: true, value: "5511912345678" });
  assert.equal(checkPhone("123").ok, false);
  assert.equal(checkGroup("123-456@g.us").ok, true);
  assert.equal(checkGroup("123@s.whatsapp.net").ok, false);
  assert.equal(checkAlias("clube-1").ok, true);
  assert.equal(checkAlias("Clube").ok, false);
  assert.equal(checkTarget("123@g.us").ok, true);
  assert.equal(checkTarget("nobody").ok, false);
});

test("a draft becomes one change per setting, with what is added and removed", () => {
  const rows = [row("whatsapp.people", "111,222"), row("whatsapp.groups", "1@g.us")];
  const changes = changesOf(rows, { "whatsapp.people": "222,333" });
  assert.equal(changes.length, 1);
  assert.deepEqual(changes[0]!.added, ["333"]);
  assert.deepEqual(changes[0]!.removed, ["111"]);
  assert.deepEqual(valuesOf(changes), { "whatsapp.people": "222,333" });
});

test("reordering or repeating what is there is no change", () => {
  assert.deepEqual(parseList(" a, b ,,a"), [...new Set(parseList(" a, b ,,a"))]);
  assert.equal(changesOf([row("whatsapp.people", "111,222")], { "whatsapp.people": "111,222" }).length, 0);
});

test("a draft that would leave WhatsApp answering nobody is caught before the controller refuses it", () => {
  const rows = [row("whatsapp.people", ""), row("whatsapp.groups", ""), row("whatsapp.own_chat", "true", { kind: "flag", options: ["true", "false"], default: "true" })];
  assert.equal(answersNobody(rows, {}), false);
  assert.equal(answersNobody(rows, { "whatsapp.own_chat": "false" }), true);
  assert.ok(Object.keys(problemsOf(rows, { "whatsapp.own_chat": "false" })).length > 0);
});

test("every setting has a section, one the screen does not know yet included", () => {
  const sections = sectionsOf([row("whatsapp.people", ""), row("discord.meme_channels", ""), row("whatsapp.something_new", "")]);
  const shown = sections.flatMap((section) => section.rows.map((each) => each.id)).sort();
  assert.deepEqual(shown, ["discord.meme_channels", "whatsapp.people", "whatsapp.something_new"]);
});

test("a refusal naming a setting points to it, and one that names none points nowhere", () => {
  assert.equal(refusedSetting("whatsapp.people: entry 2 is not a phone number", ["whatsapp.people", "whatsapp.groups"]), "whatsapp.people");
  assert.equal(refusedSetting("the database is busy", ["whatsapp.people"]), undefined);
});
