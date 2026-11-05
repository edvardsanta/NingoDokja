import test from "node:test";
import assert from "node:assert/strict";
import { formatCitations } from "../messages/citations.js";

test("product links omit repeated headings and avoid visible pipe escapes", () => {
  assert.equal(formatCitations("📚 [1] Mini Tower | Shop · Mini Tower, black, one fan, model ABC <https://example.com/case#details>"), "📚\n[1] [Mini Tower │ Shop](https://example.com/case#details)");
});

test("long titles are bounded while the complete source destination survives", () => {
  const label = "a".repeat(120);
  assert.equal(formatCitations(`📚 [1] ${label} <https://example.com/case#details>`), `📚\n[1] [${"a".repeat(99)}…](https://example.com/case#details)`);
});

test("web citations use linked titles and combine exact duplicate URLs without losing numbers", () => {
  const reply = "Price [1][6][8].\n\n📚 [1] Cooler · Cooler <https://example.com/product#details> · [6] Cooler · Cooler <https://example.com/product#price> · [8] Cooler · Cooler <https://example.com/product#price>";
  assert.equal(formatCitations(reply), "Price [1][6][8].\n\n📚\n[1] [Cooler](https://example.com/product#details)\n[6, 8] [Cooler](https://example.com/product#price)");
});

test("book references survive while web links use only the page title", () => {
  assert.equal(formatCitations("Answer\n\n📚 [1] Book · [2] Guide · Installation <https://example.com/guide>"), "Answer\n\n📚\n[1] Book\n[2] [Guide](https://example.com/guide)");
});

test("ordinary messages, unsupported links and already formatted citations are unchanged", () => {
  for (const reply of ["Visit https://example.com", "📚 [1] Book", "📚 [1] File <file:///tmp/book>", "📚 [1] Broken <https://>", "📚\n[1] [Title](https://example.com)"]) {
    assert.equal(formatCitations(reply), reply);
  }
});

test("source punctuation cannot break masked links or introduce mentions", () => {
  assert.equal(formatCitations("📚 [1] A [title] @everyone <https://example.com/a(b)>"), "📚\n[1] [A \\[title\\] ＠everyone](https://example.com/a%28b%29)");
});

test("PDF citations show the linked page and group only the same page", () => {
  const reply = "Answer [1][5][8].\n\n📚 [1] Report.pdf · Page 1 <https://example.com/report.pdf#page=1> · [5] Report.pdf · Page 5 <https://example.com/report.pdf#page=5> · [8] Report.pdf · Page 5 <https://example.com/report.pdf#page=5>";
  assert.equal(formatCitations(reply), "Answer [1][5][8].\n\n📚\n[1] [Report.pdf · p. 1](https://example.com/report.pdf#page=1)\n[5, 8] [Report.pdf · p. 5](https://example.com/report.pdf#page=5)");
});

test("PDF page remains visible when the filename is shortened", () => {
  const suffix = " · p. 123";
  assert.equal(formatCitations(`📚 [1] ${"a".repeat(120)} · Page 123 <https://example.com/report.pdf#page=123>`), `📚\n[1] [${"a".repeat(99 - suffix.length)}…${suffix}](https://example.com/report.pdf#page=123)`);
});

test("invalid page fragments do not produce a visible page locator", () => {
  for (const fragment of ["page=0", "page=-1", "page=abc"]) {
    assert.equal(formatCitations(`📚 [1] Report.pdf <https://example.com/report.pdf#${fragment}>`), `📚\n[1] [Report.pdf](https://example.com/report.pdf#${fragment})`);
  }
});
