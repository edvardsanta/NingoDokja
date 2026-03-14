import test from "node:test";
import assert from "node:assert/strict";

import { createCommands } from "../commands/commands.js";

test("createCommands exposes dokja slash commands", () => {
  const commands = createCommands("http://orch", 1000);
  assert.deepEqual(
    commands.map((command) => command.name),
    ["chat", "meme_fetch", "meme_status", "meme_refresh", "play_radio", "stop_radio", "ningo_panel", "chat_modal"],
  );
  assert.equal(commands.find((command) => command.name === "chat")?.defer, undefined);
  const playRadio = commands.find((command) => command.name === "play_radio");
  assert.ok(playRadio);
  assert.equal(playRadio.defer, false);
  assert.equal(playRadio.options?.[0]?.name, "url");
  assert.equal(playRadio.options?.[0]?.required, true);
  assert.equal(commands.find((command) => command.name === "ningo_panel")?.defer, false);
  assert.equal(commands.find((command) => command.name === "chat_modal")?.defer, false);
});

test("chat command forwards prompt and replies with orchestrator response", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (_input, init) => {
    const body = JSON.parse(String(init?.body ?? "{}"));
    assert.equal(body.event_type, "message.created");
    assert.equal(body.payload.content, "hello dokja");

    return new Response(JSON.stringify({ status: "ok", reply: "hello user" }), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  }) as typeof fetch;

  try {
    const command = createCommands("http://orch", 1000).find((entry) => entry.name === "chat");
    assert.ok(command);

    let replyText = "";
    await command.handle({
      userId: "u1",
      channelId: "c1",
      messageId: "m1",
      guildId: "g1",
      getString: () => "hello dokja",
      getNumber: () => undefined,
      reply: async (message: string) => {
        replyText = message;
      },
      updateReply: async () => {},
    });

    assert.equal(replyText, "hello user");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("play_radio command forwards the url to the voice controller", async () => {
  let replyText = "";
  let updatedReplyText = "";
  let receivedArgs: { guildId: string; userId: string; streamUrl: string } | undefined;
  let releasePlayRadio: (() => void) | undefined;

  const command = createCommands("http://orch", 1000, {
    async playRadio(args) {
      receivedArgs = args;
      await new Promise<void>((resolve) => {
        releasePlayRadio = resolve;
      });
      return "Radio started.";
    },
    async stopRadio() {
      return "Radio stopped.";
    },
  }).find((entry) => entry.name === "play_radio");

  assert.ok(command);

  await command.handle({
    userId: "u1",
    channelId: "c1",
    messageId: "m1",
    guildId: "g1",
    getString: () => "https://24493.live.streamtheworld.com/RADIO_89FM_SC",
    getNumber: () => undefined,
    reply: async (message: string) => {
      replyText = message;
    },
    updateReply: async (message) => {
      updatedReplyText = typeof message === "string" ? message : message.content;
    },
  });

  assert.equal(replyText, "Starting radio playback...");
  releasePlayRadio?.();
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.deepEqual(receivedArgs, {
    guildId: "g1",
    userId: "u1",
    streamUrl: "https://24493.live.streamtheworld.com/RADIO_89FM_SC",
  });
  assert.equal(updatedReplyText, "Radio started.");
});
