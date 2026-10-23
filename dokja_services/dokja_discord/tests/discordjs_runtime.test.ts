import test from "node:test";
import assert from "node:assert/strict";

import { splitDiscordReply } from "../runtime/discordjs_runtime.js";

test("splitDiscordReply keeps short messages unchanged", () => {
  assert.deepEqual(splitDiscordReply("hello"), ["hello"]);
});

test("splitDiscordReply splits oversized string replies into discord-safe chunks", () => {
  const message = "a".repeat(4500);
  const parts = splitDiscordReply(message);

  assert.equal(parts.length, 3);
  for (const part of parts) {
    assert.equal(typeof part, "object");
    assert.ok(part.content.length <= 2000);
  }
});

test("splitDiscordReply keeps components only on the first chunk", () => {
  const parts = splitDiscordReply({
    content: "a".repeat(2500),
    components: [
      {
        kind: "button",
        customId: "next",
        label: "Next",
      },
    ],
  });

  assert.equal(parts.length, 2);
  assert.equal(typeof parts[0], "object");
  assert.equal(typeof parts[1], "object");
  assert.ok(parts[0].components?.length);
  assert.equal(parts[1].components, undefined);
});
