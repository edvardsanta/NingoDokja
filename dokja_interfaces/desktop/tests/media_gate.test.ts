import test from "node:test";
import assert from "node:assert/strict";

import { MediaGate } from "../src/main/media_gate.js";

test("the gate knows the addresses it was given and no others", () => {
  const gate = new MediaGate();
  gate.remember(["https://images.example/a.png", "https://images.example/b.png"]);

  assert.equal(gate.has("https://images.example/a.png"), true);
  assert.equal(gate.has("https://images.example/b.png"), true);
  assert.equal(gate.has("https://images.example/c.png"), false);
  assert.equal(gate.has("https://images.example/A.png"), false);
  assert.equal(gate.has(""), false);
});

test("the gate ignores what is not an address worth keeping", () => {
  const gate = new MediaGate();
  gate.remember(["", "x".repeat(3000), 7 as unknown as string, null as unknown as string]);

  assert.equal(gate.has(""), false);
  assert.equal(gate.has("x".repeat(3000)), false);
});

test("the gate forgets the oldest addresses first and keeps one it sees again", () => {
  const gate = new MediaGate();
  gate.remember(["https://images.example/first.png"]);
  for (let index = 0; index < 499; index += 1) gate.remember([`https://images.example/${index}.png`]);
  gate.remember(["https://images.example/first.png"]); // seen again: now the newest
  gate.remember(["https://images.example/one-more.png"]);

  assert.equal(gate.has("https://images.example/first.png"), true);
  assert.equal(gate.has("https://images.example/0.png"), false, "the oldest was forgotten");
  assert.equal(gate.has("https://images.example/one-more.png"), true);
});
