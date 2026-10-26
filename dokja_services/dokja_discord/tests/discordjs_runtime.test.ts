import test from "node:test";
import assert from "node:assert/strict";

import { splitDiscordReply, toMessageContext } from "../runtime/discordjs_runtime.js";

test("message replies fetch their quoted text without changing the current message", async () => {
  const context = await toMessageContext({
    author: { id: "user", bot: false }, id: "new", channelId: "channel", guildId: "guild",
    content: "Em python mesmo", channel: { type: 0 }, mentions: { users: new Map() },
    reference: { messageId: "previous", channelId: "channel" },
    fetchReference: async () => ({ channelId: "channel", guildId: "guild", content: "A calculator like Windows?" }),
  } as any);
  assert.equal(context.content, "Em python mesmo");
  assert.equal(context.replyToContent, "A calculator like Windows?");
});

test("missing and cross-channel reply references do not prevent handling", async () => {
  for (const referenceChannel of ["channel", "other"]) {
    let fetched = false;
    const context = await toMessageContext({
      author: { id: "user", bot: false }, id: "new", channelId: "channel", guildId: "guild",
      content: "follow up", channel: { type: 0 }, mentions: { users: new Map() },
      reference: { messageId: "previous", channelId: referenceChannel },
      fetchReference: async () => { fetched = true; throw new Error("Deleted"); },
    } as any);
    assert.equal(fetched, referenceChannel === "channel");
    assert.equal(context.content, "follow up");
    assert.equal(context.replyToContent, undefined);
  }
});

test("references from another channel or guild are discarded", async () => {
  for (const target of [{ channelId: "other", guildId: "guild" }, { channelId: "channel", guildId: "other" }]) {
    const context = await toMessageContext({
      author: { id: "user", bot: false }, id: "new", channelId: "channel", guildId: "guild",
      content: "follow up", channel: { type: 0 }, mentions: { users: new Map() },
      reference: { messageId: "previous", channelId: "channel" },
      fetchReference: async () => ({ ...target, content: "unrelated" }),
    } as any);
    assert.equal(context.replyToContent, undefined);
  }
});

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

test("commands are registered on every configured server, or globally when there is none", async () => {
  const { deployCommands } = await import("../runtime/discordjs_runtime.js");
  const routes: string[] = [];
  const rest = { put: async (route: string) => { routes.push(route); } };
  const command = { name: "speak", description: "Speak", options: [], execute: async () => {} } as any;

  await deployCommands({ token: "t", applicationId: "app", devGuildIds: ["first", "second"], commands: [command] }, rest);
  assert.deepEqual(routes, ["/applications/app/guilds/first/commands", "/applications/app/guilds/second/commands"]);

  routes.length = 0;
  await deployCommands({ token: "t", applicationId: "app", devGuildIds: [], commands: [command] }, rest);
  assert.deepEqual(routes, ["/applications/app/commands"]);
});
