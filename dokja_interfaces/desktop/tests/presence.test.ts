import test from "node:test";
import assert from "node:assert/strict";

import { createPresence } from "../src/main/presence.js";

function clock() {
  let now = 1_000_000;
  return { now: () => now, advance: (ms: number) => { now += ms; } };
}

test("nobody is there until an input comes", () => {
  const time = clock();
  assert.equal(createPresence(time.now).recent(), false);
});

test("a press makes the person present for a short while, and then not", () => {
  const time = clock();
  const presence = createPresence(time.now, 3000);
  presence.note("mouseDown");
  assert.equal(presence.recent(), true);
  time.advance(3000);
  assert.equal(presence.recent(), true, "still inside the window");
  time.advance(1);
  assert.equal(presence.recent(), false, "just outside");
});

test("each press starts the window again", () => {
  const time = clock();
  const presence = createPresence(time.now, 3000);
  presence.note("keyDown");
  time.advance(2500);
  presence.note("char");
  time.advance(2500);
  assert.equal(presence.recent(), true);
});

test("a key, a click or a touch counts; a movement, a release or a scroll does not", () => {
  for (const type of ["mouseDown", "keyDown", "rawKeyDown", "char", "touchStart"]) {
    const presence = createPresence();
    presence.note(type);
    assert.equal(presence.recent(), true, type);
  }
  for (const type of ["mouseMove", "mouseUp", "keyUp", "mouseWheel", "mouseEnter", "gestureScrollBegin", "", "MOUSEDOWN"]) {
    const presence = createPresence();
    presence.note(type);
    assert.equal(presence.recent(), false, JSON.stringify(type));
  }
});
