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

test("starting over has a question that says what is lost, in both languages, and names no id", () => {
  const en = questionFor("en", "communications.link.reset", { interface: "whatsapp" });
  assert.equal(en?.message, "Unlink WhatsApp?");
  assert.match(en?.detail ?? "", /erased/);
  assert.deepEqual([en?.accept, en?.cancel], ["Unlink", "Cancel"]);

  const pt = questionFor("pt", "communications.link.reset", { interface: "whatsapp" });
  assert.equal(pt?.message, "Desvincular o WhatsApp?");
  assert.deepEqual([pt?.accept, pt?.cancel], ["Desvincular", "Cancelar"]);

  assert.equal(questionFor("en", "communications.link.start", { interface: "whatsapp" }), undefined);
  assert.equal(questionFor("en", "communications.link.status", { interface: "whatsapp" }), undefined);
});

test("a send to WhatsApp says it goes out from the owner's own account and names the conversation by its alias", () => {
  const en = questionFor("en", "communications.send", { channel_ids: ["whatsapp:friend"], content: "see you there" });
  assert.equal(en?.message, "Send on WhatsApp as you?");
  assert.match(en?.detail ?? "", /^Conversation: friend\n\nsee you there\n\n.*your own WhatsApp account/s);
  assert.doesNotMatch(en?.detail ?? "", /whatsapp:/);

  const pt = questionFor("pt", "communications.send", { channel_ids: ["whatsapp:friend"], content: "nos vemos" });
  assert.equal(pt?.message, "Enviar pelo WhatsApp como você?");
  assert.match(pt?.detail ?? "", /^Conversa: friend/);

  const discord = questionFor("en", "communications.send", { channel_ids: ["123"], content: "hi" });
  assert.equal(discord?.message, "Send to Discord as Ningo?");
});

test("feedback confirmation names the exact reference and observed result in either language",()=>{
 const en=questionFor("en","memory.resolve",{ref:"fixture:1",observed:"#Other",message:"do not show"});
 assert.equal(en?.message,"Record this feedback?");
 assert.match(en?.detail??"",/^fixture:1\n\n#Other\n\nThe first recorded outcome/);
 assert.doesNotMatch(en?.detail??"",/do not show/);
 assert.equal(questionFor("pt","memory.resolve",{ref:"fixture:1",outcome:"ignored"})?.message,"Registrar este feedback?");
});

test("deleting a conversation names it by its archive id and says what stays, in the screen's language", () => {
  const id = "a".repeat(32);
  const en = questionFor("en", "conversation.delete", { conversation_id: id, revision: 3 });
  const pt = questionFor("pt", "conversation.delete", { conversation_id: id, revision: 3 });
  assert.equal(en?.message, "Delete this conversation?");
  assert.match(en?.detail ?? "", new RegExp(`^${id}\\n\\n.*Experiences recorded from it stay`, "s"));
  assert.equal(pt?.message, "Apagar esta conversa?");
  assert.match(pt?.detail ?? "", /As experiências registradas a partir dela continuam/);
  assert.equal(pt?.accept, "Apagar");
});

test("deleting a kept fact asks in the screen's language and says the conversation stays", () => {
  assert.equal(questionFor("en", "conversation.fact_delete", { fact_id: "a".repeat(32) })?.message, "Delete this kept fact?");
  const pt = questionFor("pt", "conversation.fact_delete", { fact_id: "a".repeat(32) });
  assert.equal(pt?.message, "Apagar este fato guardado?");
  assert.match(pt?.detail ?? "", /A conversa em que foi dito continua/);
  assert.equal(pt?.accept, "Apagar");
});
