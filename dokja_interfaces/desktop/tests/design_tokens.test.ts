import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { test } from "node:test";

test("every design token resolves through a defined, acyclic dependency graph", async () => {
  const source = await readFile(new URL("../src/renderer/tokens.css", import.meta.url), "utf8");
  const declarations = [...source.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)];
  const values = new Map(declarations.map(([, name, value]) => [name!, value!]));
  assert.ok(values.size > 0, "the token stylesheet must define its roles");
  assert.equal(values.size, declarations.length, "duplicate declarations hide conflicting defaults");
  const checked = new Set<string>();
  function resolve(name: string, path: readonly string[] = []) {
    assert.ok(!path.includes(name), `circular token reference: ${[...path, name].join(" -> ")}`);
    const value = values.get(name);
    assert.ok(value, `undefined token ${name} referenced by ${path.at(-1) ?? "consumer"}`);
    if (checked.has(name)) return;
    for (const [, dependency] of value.matchAll(/var\(\s*(--[\w-]+)/g)) {
      resolve(dependency!, [...path, name]);
    }
    checked.add(name);
  }
  for (const name of values.keys()) resolve(name);
});
