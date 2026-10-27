import test from "node:test";
import assert from "node:assert/strict";

import { splitDiscordReply, toMessageContext, toMessageReply } from "../runtime/discordjs_runtime.js";

test("long formula replies attach the PNG only to their first chunk", () => {
  const attachment = { filename: "formula-1.png", content_type: "image/png" as const, data_b64: Buffer.from("png bytes").toString("base64") };
  const parts = splitDiscordReply({ content: "x".repeat(4500), attachments: [attachment] });
  assert.equal(parts.length, 3);
  assert.deepEqual(typeof parts[0] === "object" && parts[0].attachments, [attachment]);
  assert.ok(parts.slice(1).every(part => typeof part === "object" && part.attachments === undefined));
  const payload = toMessageReply(parts[0]);
  assert.equal(payload.files?.[0].name, "formula-1.png");
  assert.deepEqual(payload.files?.[0].attachment, Buffer.from("png bytes"));
});

test("message replies fetch their quoted text without changing the current message", async () => {
  const context = await toMessageContext({
    author: { id: "user", bot: false }, id: "new", channelId: "channel", guildId: "guild",
    content: "Em python mesmo", channel: { type: 0 }, mentions: { users: new Map([["mentioned", { id: "mentioned" }]]) },
    reference: { messageId: "previous", channelId: "channel" },
    fetchReference: async () => ({ channelId: "channel", guildId: "guild", content: "A calculator like Windows?" }),
  } as any);
  assert.equal(context.content, "Em python mesmo");
  assert.equal(context.replyToContent, "A calculator like Windows?");
  assert.deepEqual(context.mentionedUserIds, ["mentioned"]);
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

function contents(message: string): string[] {
  return splitDiscordReply(message).map(part => typeof part === "string" ? part : part.content);
}

test("long Python replies retain fences, language, indentation and blank lines", () => {
  const source = 'class Calculadora:\n' + '    def calcular(self):\n        return "😀"\n\n'.repeat(130);
  const parts = contents(`\`\`\`python\n${source}\`\`\``);

  assert.ok(parts.length > 2);
  for (const part of parts) {
    assert.ok(part.length <= 2000);
    assert.ok(part.startsWith("```python\n"));
    assert.ok(part.endsWith("\n```"));
  }
  const restored = parts.map((part, index) => {
    const body = part.slice("```python\n".length, -3);
    return index < parts.length - 1 ? body.slice(0, -1) : body;
  }).join("");
  assert.equal(restored, source);
});

test("code fences do not leak into the explanation after the code", () => {
  const source = '    print("hello")\n'.repeat(160);
  const explanation = "Run the calculator. ".repeat(130);
  const parts = contents(`Example:\n\`\`\`python\n${source}\`\`\`\n${explanation}`);
  assert.ok(parts.every(part => part.length <= 2000));
  assert.ok(parts.every(part => (part.match(/^```/gm) ?? []).length % 2 === 0));
  assert.ok(parts.at(-1)?.includes("Run the calculator."));
  assert.ok(!parts.at(-1)?.startsWith("```"));
});

test("long plain text preserves spaces and newlines", () => {
  const message = "  text with spaces  \n\n".repeat(300);
  assert.equal(contents(message).join(""), message);
});

test("oversized code lines preserve Unicode and stay within the limit", () => {
  const parts = contents('```python\n    value = "' + "😀".repeat(2500) + '"\n```');
  for (const part of parts) {
    assert.ok(part.length <= 2000);
    assert.equal(part.isWellFormed(), true);
    assert.ok(part.startsWith("```python\n"));
    assert.ok(part.endsWith("\n```"));
  }
});

test("tilde fences retain their language across messages", () => {
  const parts = contents("~~~python\n" + "    pass\n".repeat(600) + "~~~");
  assert.ok(parts.length > 1);
  assert.ok(parts.every(part => part.startsWith("~~~python\n") && part.endsWith("\n~~~")));
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

test("an embed keeps a picture only from an http(s) address and bounds its text", async () => {
  const { toEmbed } = await import("../runtime/discordjs_runtime.js");
  assert.deepEqual(toEmbed({ title: "t", description: "d", thumbnailUrl: "https://covers.example/a.jpg" }), { title: "t", description: "d", thumbnail: { url: "https://covers.example/a.jpg" } });
  assert.deepEqual(toEmbed({ title: "t", thumbnailUrl: "file:///etc/passwd" }), { title: "t" });
  assert.equal(toEmbed({ title: "x".repeat(300) }).title?.length, 256);
});
