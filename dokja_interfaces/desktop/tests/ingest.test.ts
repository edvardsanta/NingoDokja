import test from "node:test";
import assert from "node:assert/strict";

import { baseName, buildIngest, cleanTags } from "../src/main/ingest.js";
import { INGEST_LIMITS } from "../src/shared/ingest.js";

const build = (raw: Record<string, unknown>) => buildIngest(raw);

test("a note is a title and a text, with a kind and an id the shell chose", () => {
  const sent = build({ mode: "note", title: "  Notes on Kant ", body: "The categorical imperative.\nSecond line.", tags: "ethics, kant" });
  assert.deepEqual({ ...sent, source_id: "x" }, {
    title: "Notes on Kant",
    body: "The categorical imperative.\nSecond line.",
    kind: "note",
    tags: ["ethics", "kant"],
    source_id: "x",
  });
  assert.match(String(sent?.source_id), /^note:notes-on-kant-[0-9a-f]{10}$/);
});

test("the same note has the same id, a note with the same title and another text has another", () => {
  const first = build({ mode: "note", title: "Idea", body: "one" });
  const again = build({ mode: "note", title: "Idea", body: "one" });
  const other = build({ mode: "note", title: "Idea", body: "two" });
  assert.equal(first?.source_id, again?.source_id);
  assert.notEqual(first?.source_id, other?.source_id);
});

test("the id keeps to what the service allows, whatever the title", () => {
  for (const title of ["Ideia sobre a liberdade", "  ", "!!!", "Caf\u00e9 \u2014 \u00e0 la carte", "a/b#c@d", "x".repeat(300)]) {
    const sent = build({ mode: "note", title: title.trim() === "" ? "ok" : title, body: "text" });
    assert.match(String(sent?.source_id), /^note:[a-z0-9-]*-[0-9a-f]{10}$/, JSON.stringify(title));
    assert.ok(String(sent?.source_id).length <= 80);
  }
  assert.match(String(build({ mode: "note", title: "Ideia sobre a liberdade", body: "x" })?.source_id), /^note:ideia-sobre-a-liberdade-/);
  assert.match(String(build({ mode: "note", title: "!!!", body: "x" })?.source_id), /^note:note-/);
});

test("a note needs a title and a text and has a size", () => {
  for (const raw of [
    { mode: "note", body: "text" },
    { mode: "note", title: "  ", body: "text" },
    { mode: "note", title: "T" },
    { mode: "note", title: "T", body: "   \n " },
    { mode: "note", title: "T", body: 5 },
    { mode: "note", title: "x".repeat(INGEST_LIMITS.titleChars + 1), body: "text" },
    { mode: "note", title: "T", body: "x".repeat(INGEST_LIMITS.noteChars + 1) },
  ]) {
    assert.equal(build(raw), undefined, JSON.stringify(raw).slice(0, 80));
  }
  assert.ok(build({ mode: "note", title: "T", body: "x".repeat(INGEST_LIMITS.noteChars) }));
  assert.ok(build({ mode: "note", title: "x".repeat(INGEST_LIMITS.titleChars), body: "text" }));
});

test("an address must be a web address, because anything else would call one of the owner's plugins", () => {
  const sent = build({ mode: "address", address: " https://example.com/a?b=1#c ", title: "A page", tags: ["web"] });
  assert.deepEqual(sent, { source: "https://example.com/a?b=1#c", kind: "article", title: "A page", tags: ["web"] });
  assert.deepEqual(build({ mode: "address", address: "http://example.com" }), { source: "http://example.com/", kind: "article" });
  // what is sent is the address as a browser reads it, not the text that was typed
  assert.equal(build({ mode: "address", address: "https:///path" })?.source, "https://path/");

  for (const address of [
    "", "   ", "example.com/a", "plugin:something", "myscheme:anything", "file:///etc/passwd", "ftp://example.com/a",
    "javascript:alert(1)", "data:text/plain,hi", "/relative/path", "https://user:secret@example.com/", "https://user@example.com/",
    "http://", "https://", "https://example.com/" + "a".repeat(INGEST_LIMITS.addressChars), 5, null, undefined, ["https://example.com"],
  ]) {
    assert.equal(build({ mode: "address", address }), undefined, JSON.stringify(address)?.slice(0, 60));
  }
});

test("a file is a name, a content and an id made of both", () => {
  const sent = build({ mode: "file", filename: "My Reading.PDF", content: "QUJD", title: "A reading" });
  assert.equal(sent?.filename, "My Reading.PDF");
  assert.equal(sent?.content_b64, "QUJD");
  assert.equal(sent?.kind, "document");
  assert.equal(sent?.title, "A reading");
  assert.match(String(sent?.source_id), /^file:my-reading-[0-9a-f]{10}$/);

  const other = build({ mode: "file", filename: "My Reading.PDF", content: "QUJE" });
  assert.notEqual(other?.source_id, sent?.source_id, "another content is another document, not a silent replacement");
  assert.equal(build({ mode: "file", filename: "My Reading.PDF", content: "QUJD" })?.source_id, sent?.source_id);
});

test("a file is refused when its content is not base64, empty or too big, or its name is not a name", () => {
  const big = "A".repeat(Math.ceil(INGEST_LIMITS.fileBytes / 3) * 4 + 4);
  for (const raw of [
    { mode: "file", filename: "a.txt", content: "" },
    { mode: "file", filename: "a.txt", content: "not base64 !!" },
    { mode: "file", filename: "a.txt", content: "QUJD\nQUJD" },
    { mode: "file", filename: "a.txt", content: "=QUJD" },
    { mode: "file", filename: "a.txt", content: big },
    { mode: "file", filename: "a.txt" },
    { mode: "file", content: "QUJD" },
    { mode: "file", filename: "", content: "QUJD" },
    { mode: "file", filename: "..", content: "QUJD" },
    { mode: "file", filename: "a/", content: "QUJD" },
    { mode: "file", filename: "a.txt", content: 5 },
  ]) {
    assert.equal(build(raw), undefined, JSON.stringify(raw).slice(0, 80));
  }
  assert.ok(build({ mode: "file", filename: "a.txt", content: "A".repeat(Math.ceil(INGEST_LIMITS.fileBytes / 3) * 4) }));
});

test("only the name of a file is sent, never the path in front of it", () => {
  assert.equal(baseName("../../etc/passwd.txt"), "passwd.txt");
  assert.equal(baseName("C:\\Users\\me\\reading.docx"), "reading.docx");
  assert.equal(baseName("/home/me/book.epub"), "book.epub");
  assert.equal(baseName("plain.md"), "plain.md");
  assert.equal(baseName("  spaced name.txt  "), "spaced name.txt");
  for (const raw of ["", "  ", ".", "..", "dir/", "dir\\", undefined, 5]) assert.equal(baseName(raw), undefined, JSON.stringify(raw));
  const long = baseName("x".repeat(500) + ".pdf") ?? "";
  assert.ok(long.length <= INGEST_LIMITS.filenameChars && long.endsWith(".pdf"));
});

test("the screen cannot choose the kind, the id, the reference or the source of a note or a file", () => {
  const extras = { kind: "secret", source_id: "other:doc", source_ref: "elsewhere", source: "plugin:x", unknown: 1 };
  const note = build({ mode: "note", title: "T", body: "text", ...extras }) ?? {};
  const file = build({ mode: "file", filename: "a.txt", content: "QUJD", ...extras }) ?? {};
  for (const sent of [note, file]) {
    assert.notEqual(sent.source_id, "other:doc");
    assert.equal(sent.source_ref, undefined);
    assert.equal(sent.source, undefined);
    assert.equal(sent.unknown, undefined);
    assert.notEqual(sent.kind, "secret");
  }
  const address = build({ mode: "address", address: "https://example.com/", ...extras }) ?? {};
  assert.equal(address.source, "https://example.com/");
  assert.equal(address.source_id, undefined);
  assert.equal(address.kind, "article");
});

test("an unknown mode is refused", () => {
  for (const mode of [undefined, "", "NOTE", "text", "ingest", 5, null, ["note"]]) {
    assert.equal(build({ mode, title: "T", body: "b", address: "https://example.com/" }), undefined, JSON.stringify(mode));
  }
});

test("tags are a few short words, from a list or from text split at commas", () => {
  assert.deepEqual(cleanTags(undefined), []);
  assert.deepEqual(cleanTags(null), []);
  assert.deepEqual(cleanTags(" a, b ,, a ,c "), ["a", "b", "c"]);
  assert.deepEqual(cleanTags([" x ", "y", "", "x"]), ["x", "y"]);
  assert.equal(cleanTags(Array.from({ length: INGEST_LIMITS.tags + 1 }, (_, i) => `t${i}`)), undefined);
  assert.equal(cleanTags(Array.from({ length: INGEST_LIMITS.tags }, (_, i) => `t${i}`))?.length, INGEST_LIMITS.tags);
  assert.equal(cleanTags("x".repeat(INGEST_LIMITS.tagChars + 1)), undefined);
  assert.equal(cleanTags(["has,comma"]), undefined);
  assert.equal(cleanTags(5), undefined);
  assert.equal(cleanTags({ a: 1 }), undefined);
});
