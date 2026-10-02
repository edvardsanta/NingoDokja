import test from "node:test";
import assert from "node:assert/strict";

import { createConfirmer, questionFor, type Question } from "../src/main/dialogs.js";

test("only a deletion has a question, and it names the id the shell will send", () => {
  const question = questionFor("en", "knowledge.delete", { source_id: "note:a-1" });
  assert.ok(question);
  assert.equal(question.message, "Delete this document?");
  assert.match(question.detail, /^note:a-1\n/);
  assert.match(question.detail, /cannot be undone/);
  assert.deepEqual([question.accept, question.cancel], ["Delete", "Cancel"]);

  const pt = questionFor("pt", "knowledge.delete", { source_id: "note:a-1" });
  assert.deepEqual([pt?.message, pt?.accept, pt?.cancel], ["Apagar este documento?", "Apagar", "Cancelar"]);

  for (const type of ["knowledge.ingest", "knowledge.reindex", "knowledge.list", "ningo.status"] as const) {
    assert.equal(questionFor("en", type, { source_id: "x" }), undefined, type);
  }
});

test("the question names the id, never a title or any other text that came with it", () => {
  const question = questionFor("en", "knowledge.delete", { source_id: "note:a-1", title: "Harmless holiday photos", message: "Delete?" });
  assert.match(question?.detail ?? "", /^note:a-1\n/);
  assert.doesNotMatch(question?.detail ?? "", /Harmless|holiday/);
  assert.equal(question?.message, "Delete this document?");
});

test("the confirmer answers what the person answered", async () => {
  const shown: Question[] = [];
  const yes = createConfirmer("en", async (question) => {
    shown.push(question);
    return true;
  });
  assert.equal(await yes("knowledge.delete", { source_id: "note:a-1" }), true);
  assert.equal(shown.length, 1);

  const no = createConfirmer("en", async () => false);
  assert.equal(await no("knowledge.delete", { source_id: "note:a-1" }), false);
});

test("an action with no question is never confirmed, and nothing is shown for it", async () => {
  let shown = 0;
  const confirm = createConfirmer("en", async () => {
    shown += 1;
    return true;
  });
  assert.equal(await confirm("knowledge.ingest", {}), false);
  assert.equal(shown, 0);
});

test("while a question is open another one is a no, and the next can be asked after", { timeout: 2000 }, async () => {
  let release: (answer: boolean) => void = () => undefined;
  let shown = 0;
  const confirm = createConfirmer("en", () => {
    shown += 1;
    return new Promise<boolean>((resolve) => {
      release = resolve;
    });
  });

  const first = confirm("knowledge.delete", { source_id: "a" });
  assert.equal(await confirm("knowledge.delete", { source_id: "b" }), false, "no second window over the first");
  assert.equal(shown, 1);
  release(true);
  assert.equal(await first, true);

  const third = confirm("knowledge.delete", { source_id: "c" });
  assert.equal(shown, 2, "the window is free again");
  release(false);
  assert.equal(await third, false);
});

test("a window that fails to open leaves the confirmer usable and is not a yes", async () => {
  let attempts = 0;
  const confirm = createConfirmer("en", async () => {
    attempts += 1;
    if (attempts === 1) throw new Error("no window");
    return true;
  });
  await assert.rejects(confirm("knowledge.delete", { source_id: "a" }), /no window/);
  assert.equal(await confirm("knowledge.delete", { source_id: "a" }), true);
});
