import test from "node:test";
import assert from "node:assert/strict";

import { citedParts, detailedSources, originOf, splitSources } from "../src/renderer/cards/reply_sources_model.js";

test("a reply closed by the sources line gives its body and its documents, each once", () => {
  const split = splitSources("It holds[1], and so does the other[2].\n\n📚 [1] First text · [2] Second text");
  assert.equal(split.body, "It holds[1], and so does the other[2].");
  assert.deepEqual(split.sources, [{ number: 1, title: "First text" }, { number: 2, title: "Second text" }]);
});

test("a reply without the line is left as it is", () => {
  for (const reply of ["Plain answer.", "", "Two\n\nparagraphs [1] but no sources.", "Talks of 📚 [1] mid-line."]) {
    assert.deepEqual(splitSources(reply), { body: reply, sources: [] });
  }
});

test("a title with a separator in it stays whole; only a number that goes up starts an entry", () => {
  assert.deepEqual(splitSources("Yes[1].\n\n📚 [1] Mind · Body · [2] Notes on [3] things · [2] Other").sources, [
    { number: 1, title: "Mind · Body" },
    { number: 2, title: "Notes on [3] things · [2] Other" },
  ]);
});

test("only the sources line is taken out when a kept fact's line follows it", () => {
  const split = splitSources("Answer[1].\n\n📚 [1] A text\n\nKept: you like tea.");
  assert.equal(split.body, "Answer[1].\n\nKept: you like tea.");
  assert.deepEqual(split.sources, [{ number: 1, title: "A text" }]);
});

test("a reply that is only the sources line has an empty body", () => {
  assert.deepEqual(splitSources("📚 [1] A text"), { body: "", sources: [{ number: 1, title: "A text" }] });
});

// The line exactly as the orchestrator writes it (citeSources), the empty title included: "[%d] %s" joined by " · ".
test("reads the line as the orchestrator writes it", () => {
  assert.deepEqual(splitSources("model reply\n\n\u{1F4DA} [1] The case \u00B7 [2] Another"), {
    body: "model reply",
    sources: [{ number: 1, title: "The case" }, { number: 2, title: "Another" }],
  });
  assert.deepEqual(splitSources("model reply\n\n\u{1F4DA} [1]  \u00B7 [2] Named").sources, [{ number: 1, title: "" }, { number: 2, title: "Named" }]);
  assert.deepEqual(splitSources("model reply\n\n\u{1F4DA} [1] ").sources, [{ number: 1, title: "" }]);
});

test("a document without a title is listed by its number", () => {
  assert.deepEqual(splitSources("Yes[1][2].\n\n📚 [1] · [2] Named").sources, [{ number: 1, title: "" }, { number: 2, title: "Named" }]);
});

test("a line that does not start with a number is not read as sources", () => {
  assert.deepEqual(splitSources("Body.\n\n📚 sources: [1] A text"), { body: "Body.\n\n📚 sources: [1] A text", sources: [] });
});

test("a title is only ever text: markup in it comes back unchanged", () => {
  assert.equal(splitSources("Ok[1].\n\n📚 [1] <img src=x onerror=alert(1)>").sources[0]?.title, "<img src=x onerror=alert(1)>");
});

test("citations of listed documents are marked, any other bracket stays text", () => {
  const sources = [{ number: 1, title: "A" }, { number: 2, title: "B" }];
  assert.deepEqual(citedParts("See[1][2], also [1, 2] and [7] or arr[0].", sources), [
    "See",
    { cite: [1], text: "[1]" },
    { cite: [2], text: "[2]" },
    ", also ",
    { cite: [1, 2], text: "[1, 2]" },
    " and [7] or arr[0].",
  ]);
  assert.deepEqual(citedParts("No marks here.", sources), ["No marks here."]);
});

test("the archive's sources win over the line, and a reply without them keeps the line's", () => {
  const written = [{ number: 1, title: "From the line" }];
  const kept = [{ number: 1, title: "Kept", id: "note:one" }];
  assert.deepEqual(detailedSources(written, kept), kept);
  assert.deepEqual(detailedSources(written, undefined), written);
  assert.deepEqual(detailedSources(written, []), written);
});

test("a document's origin is its host, or the reference as written", () => {
  assert.equal(originOf("https://www.example.com/some/long/path?x=1"), "example.com");
  assert.equal(originOf("a book, page 12"), "a book, page 12");
  assert.equal(originOf("javascript:alert(1)"), "javascript:alert(1)");
  assert.equal(originOf(""), "");
  assert.equal(originOf(undefined), "");
});

