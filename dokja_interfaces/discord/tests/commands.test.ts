import test from "node:test";
import assert from "node:assert/strict";

import { createCommands } from "../commands/commands.js";

test("createCommands exposes dokja slash commands", () => {
  const commands = createCommands("http://orch", 1000);
  assert.deepEqual(
    commands.map((command) => command.name),
    [
      "chat",
      "meme_fetch",
      "meme_status",
      "meme_refresh",
      "speak",
      "voice_chat_start",
      "wake_chat",
      "voice_chat_stop",
      "play_radio",
      "stop_radio",
      "ningo_panel",
      "chat_modal",
    ],
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
    let ephemeral: boolean | undefined;
    await command.handle({
      userId: "u1",
      channelId: "c1",
      messageId: "m1",
      guildId: "g1",
      getString: () => "hello dokja",
      getNumber: () => undefined,
      reply: async (message: string, opts?: { ephemeral?: boolean }) => {
        replyText = message;
        ephemeral = opts?.ephemeral;
      },
      updateReply: async () => {},
      showModal: async () => {},
    });

    assert.equal(replyText, "hello user");
    assert.equal(ephemeral, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("play_radio command forwards the url to the voice controller", async () => {
  let replyText = "";
  let replyEphemeral: boolean | undefined;
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
    async startConversation() {
      return "Conversation started.";
    },
    async stopConversation() {
      return "Conversation stopped.";
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
    reply: async (message: string, opts?: { ephemeral?: boolean }) => {
      replyText = message;
      replyEphemeral = opts?.ephemeral;
    },
    updateReply: async (message) => {
      updatedReplyText = typeof message === "string" ? message : message.content;
    },
    showModal: async () => {},
  });

  assert.equal(replyText, "Starting radio playback...");
  assert.equal(replyEphemeral, true);
  releasePlayRadio?.();
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.deepEqual(receivedArgs, {
    guildId: "g1",
    userId: "u1",
    streamUrl: "https://24493.live.streamtheworld.com/RADIO_89FM_SC",
  });
  assert.equal(updatedReplyText, "Radio started.");
});

test("speak command forwards text to the voice controller", async () => {
  let replyText = "";
  let replyEphemeral: boolean | undefined;
  let updatedReplyText = "";
  let receivedArgs: { guildId: string; userId: string; text: string } | undefined;
  let releaseSpeak: (() => void) | undefined;

  const command = createCommands("http://orch", 1000, {
    async speakText(args) {
      receivedArgs = args;
      await new Promise<void>((resolve) => {
        releaseSpeak = resolve;
      });
      return "Speaking in <#g1>";
    },
    async playRadio() {
      return "Radio started.";
    },
    async stopRadio() {
      return "Radio stopped.";
    },
    async startConversation() {
      return "Conversation started.";
    },
    async stopConversation() {
      return "Conversation stopped.";
    },
  }).find((entry) => entry.name === "speak");

  assert.ok(command);

  await command.handle({
    userId: "u1",
    channelId: "c1",
    messageId: "m1",
    guildId: "g1",
    getString: () => "ola ningo",
    getNumber: () => undefined,
    reply: async (message: string, opts?: { ephemeral?: boolean }) => {
      replyText = message;
      replyEphemeral = opts?.ephemeral;
    },
    updateReply: async (message) => {
      updatedReplyText = typeof message === "string" ? message : message.content;
    },
    showModal: async () => {},
  });

  assert.equal(replyText, "Starting speech playback...");
  assert.equal(replyEphemeral, true);
  releaseSpeak?.();
  await new Promise((resolve) => setTimeout(resolve, 0));

  assert.deepEqual(receivedArgs, {
    guildId: "g1",
    userId: "u1",
    text: "ola ningo",
  });
  assert.equal(updatedReplyText, "Speaking in <#g1>");
});

test("voice_chat_start starts listening through the voice controller", async () => {
  let replyText = "";
  let updatedReplyText = "";
  let receivedArgs:
    | { guildId: string; userId: string; onTurn: (transcript: string) => Promise<void> }
    | undefined;

  const command = createCommands("http://orch", 1000, {
    async speakText() {
      return "Speaking in <#g1>";
    },
    async playRadio() {
      return "Radio started.";
    },
    async stopRadio() {
      return "Radio stopped.";
    },
    async startConversation(args) {
      receivedArgs = args;
      return "Listening to your voice in <#g1>";
    },
    async stopConversation() {
      return "Conversation stopped.";
    },
  }).find((entry) => entry.name === "voice_chat_start");

  assert.ok(command);

  await command.handle({
    userId: "u1",
    channelId: "c1",
    messageId: "m1",
    guildId: "g1",
    getString: () => "",
    getNumber: () => undefined,
    reply: async (message: string) => {
      replyText = message;
    },
    updateReply: async (message) => {
      updatedReplyText = typeof message === "string" ? message : message.content;
    },
    showModal: async () => {},
  });

  assert.equal(replyText, "Starting voice conversation...");
  assert.equal(receivedArgs?.guildId, "g1");
  assert.equal(receivedArgs?.userId, "u1");
  assert.equal(updatedReplyText, "Listening to your voice in <#g1>");
});

test("wake_chat starts listening through the wakeword mode", async () => {
  let updatedReplyText = "";
  let activationMode: "always" | "wakeword" | undefined;

  const command = createCommands("http://orch", 1000, {
    async speakText() {
      return "Speaking in <#g1>";
    },
    async playRadio() {
      return "Radio started.";
    },
    async stopRadio() {
      return "Radio stopped.";
    },
    async startConversation(args) {
      activationMode = args.activationMode;
      return "Listening for the wake word in <#g1>";
    },
    async stopConversation() {
      return "Conversation stopped.";
    },
  }).find((entry) => entry.name === "wake_chat");

  assert.ok(command);

  await command.handle({
    userId: "u1",
    channelId: "c1",
    messageId: "m1",
    guildId: "g1",
    getString: () => "",
    getNumber: () => undefined,
    reply: async () => {},
    updateReply: async (message) => {
      updatedReplyText = typeof message === "string" ? message : message.content;
    },
    showModal: async () => {},
  });

  assert.equal(activationMode, "wakeword");
  assert.equal(updatedReplyText, "Listening for the wake word in <#g1>");
});
