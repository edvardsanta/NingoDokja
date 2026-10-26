import test from "node:test";
import assert from "node:assert/strict";

import { withoutLinkCodes } from "../src/renderer/cards/log_text.js";

const NOTE = "[link code hidden]";
const prefix = "[dokja-whatsapp] | 2026-10-25T12:16:16.918145966-03:00 ";
// A drawn code: rows of half blocks, as the WhatsApp interface prints one.
const pattern = ["█▀▀▀▀▀█ ▄▀▄ █▀▀▀▀▀█", "█ ███ █ ▀▄▀ █ ███ █", "█ ▀▀▀ █ █▀█ █ ▀▀▀ █", "▀▀▀▀▀▀▀ █ ▀ ▀▀▀▀▀▀▀", "▄▀█▄ ▀▀▄█▀▄▀█▄ ▀█▄▀", "▀▀▀▀▀▀▀ ▀ ▀ ▀ ▀▀▀ ▀"];
const code = pattern.map((row) => prefix + row);

test("a drawn link code becomes one line that keeps the log's prefix", () => {
  const log = [prefix + "waiting for link", ...code, prefix + "linked"].join("\n");
  assert.equal(withoutLinkCodes(log, NOTE), [prefix + "waiting for link", prefix + NOTE, prefix + "linked"].join("\n"));
});

test("works without a prefix, and every code in the log is hidden", () => {
  const bare = pattern.join("\n");
  assert.equal(withoutLinkCodes(`${bare}\nbetween\n${bare}`, NOTE), `${NOTE}\nbetween\n${NOTE}`);
});

test("leaves ordinary lines, short block art and a log without codes as they came", () => {
  const ordinary = [prefix + "started", prefix + "██████████ 100%", prefix + "done"].join("\n");
  assert.equal(withoutLinkCodes(ordinary, NOTE), ordinary);
  const fewRows = code.slice(0, 3).join("\n");
  assert.equal(withoutLinkCodes(fewRows, NOTE), fewRows, "three rows are not a code");
  const divider = Array.from({ length: 6 }, () => prefix + "▀▀▀").join("\n");
  assert.equal(withoutLinkCodes(divider, NOTE), divider, "rows too short to be a code stay");
  assert.equal(withoutLinkCodes("", NOTE), "");
});
