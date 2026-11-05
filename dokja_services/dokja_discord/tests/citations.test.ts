import test from "node:test";
import assert from "node:assert/strict";
import { formatCitations } from "../messages/citations.js";

test("web citations use linked titles and combine exact duplicate URLs without losing numbers", () => {
  const reply = "Price [1][6][8].\n\n📚 [1] Cooler · Cooler <https://example.com/product#details> · [6] Cooler · Cooler <https://example.com/product#price> · [8] Cooler · Cooler <https://example.com/product#price>";
  assert.equal(formatCitations(reply), "Price [1][6][8].\n\n📚\n[1] [Cooler](<https://example.com/product#details>)\n[6, 8] [Cooler](<https://example.com/product#price>)");
});

test("book locators and differing headings survive mixed citation formatting", () => {
  assert.equal(formatCitations("Answer\n\n📚 [1] Book · [2] Guide · Installation <https://example.com/guide>"), "Answer\n\n📚\n[1] Book\n[2] [Guide · Installation](<https://example.com/guide>)");
});

test("ordinary messages, unsupported links and already formatted citations are unchanged", () => {
  for (const reply of ["Visit https://example.com", "📚 [1] Book", "📚 [1] File <file:///tmp/book>", "📚 [1] Broken <https://>", "📚\n[1] [Title](<https://example.com>)"]) {
    assert.equal(formatCitations(reply), reply);
  }
});

test("source punctuation cannot break masked links or introduce mentions", () => {
  assert.equal(formatCitations("📚 [1] A [title] @everyone <https://example.com/a(b)>"), "📚\n[1] [A \\[title\\] ＠everyone](<https://example.com/a%28b%29>)");
});
