import assert from "node:assert/strict";
import test from "node:test";

import { createMusicCommands } from "../music/commands.js";
import type { DiscordCommandContext } from "../types.js";

type Sent = { eventType: string; content: string; channelId: string; guildId: string; userId: string };

// A voice controller that records what it is asked to play, with the person's channel and the
// channel Ningo plays in set by the test.
function fakeVoice(member: string | undefined, playing?: string) {
  const applied: unknown[] = [];
  let hooks: { onEnded(entry: number): void; onGone(): void } | undefined;
  const voice = {
    async memberVoiceChannel() { return member; },
    musicChannel() { return playing; },
    async applyMusic(args: { state: unknown; hooks: typeof hooks }) {
      applied.push(args.state);
      hooks = args.hooks;
    },
  };
  return { voice: voice as never, applied, hooks: () => hooks! };
}

function context(overrides: Partial<DiscordCommandContext> & { song?: string; percent?: number } = {}) {
  const replies: string[] = [];
  const ctx = {
    userId: "u1", channelId: "text1", messageId: "m1", guildId: "g1",
    getString: () => overrides.song ?? "",
    getNumber: () => overrides.percent,
    reply: async (message: string) => { replies.push(message); },
    updateReply: async (message: unknown) => { replies.push(typeof message === "string" ? message : String((message as { content: string }).content)); },
    showModal: async () => {},
    ...overrides,
  } as DiscordCommandContext;
  return { ctx, replies };
}

const queue = (version: number, entry?: number) => ({
  ok: true, epoch: "e1", version, paused: false, volume: 100,
  ...(entry ? { now_playing: { entry, url: `https://play.example/${entry}`, title: "Monalisa", uploader: "Jorge" } } : {}),
});

function commands(voice: unknown, replies: Record<string, { reply: string; music?: unknown }>, sent: Sent[]) {
  return createMusicCommands({
    orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: voice as never,
    dispatch: async (eventType, request) => {
      sent.push({ eventType, ...request });
      return replies[eventType] ?? { reply: "" };
    },
  });
}
const find = (list: ReturnType<typeof commands>, name: string) => list.find((entry) => entry.name === name)!;

test("play answers at once, sends the words, and plays what the queue says", async () => {
  const sent: Sent[] = [];
  const { voice, applied } = fakeVoice("voice1");
  const list = commands(voice, { "music.play": { reply: "Now playing: Monalisa – Jorge.", music: queue(1, 1) } }, sent);
  const { ctx, replies } = context({ song: "monalisa jorge" });
  await find(list, "play").handle(ctx);
  assert.equal(replies[0], "Looking for it...", "answered before searching");
  assert.equal(replies.at(-1), "Now playing: Monalisa – Jorge.");
  assert.deepEqual(sent, [{ eventType: "music.play", content: "monalisa jorge", userId: "u1", channelId: "text1", guildId: "g1" }]);
  assert.equal(applied.length, 1);
});

test("play needs the person in a voice channel, and the one where music already plays", async () => {
  const sent: Sent[] = [];
  let { ctx, replies } = context({ song: "x" });
  await find(commands(fakeVoice(undefined).voice, {}, sent), "play").handle(ctx);
  assert.equal(replies.at(-1), "Join a voice channel to play music.");
  ({ ctx, replies } = context({ song: "x" }));
  await find(commands(fakeVoice("voice2", "voice1").voice, {}, sent), "play").handle(ctx);
  assert.match(replies.at(-1)!, /Music is playing in <#voice1>/);
  ({ ctx, replies } = context());
  await find(commands(fakeVoice("voice2", "voice1").voice, {}, sent), "skip").handle(ctx);
  assert.match(replies.at(-1)!, /Join it to control the queue/);
  assert.deepEqual(sent, [], "nothing reached the orchestrator");
});

test("the queue can be read from anywhere, and the volume is sent as typed", async () => {
  const sent: Sent[] = [];
  const list = commands(fakeVoice(undefined, "voice1").voice, { "music.queue": { reply: "Nothing is playing." }, "music.volume": { reply: "Volume: 40%." } }, sent);
  let { ctx, replies } = context();
  await find(list, "queue").handle(ctx);
  assert.equal(replies.at(-1), "Nothing is playing.");
  ({ ctx, replies } = context({ percent: 40 }));
  await find(commands(fakeVoice("voice1", "voice1").voice, { "music.volume": { reply: "Volume: 40%." } }, sent), "volume").handle(ctx);
  assert.equal(sent.at(-1)?.content, "40");
});

test("a track that ends is reported from the channel of the last command, and what comes next plays", async () => {
  const sent: Sent[] = [];
  const fake = fakeVoice("voice1");
  const list = commands(fake.voice, {
    "music.play": { reply: "Now playing.", music: queue(1, 1) },
    "music.ended": { reply: "", music: queue(2, 2) },
  }, sent);
  await find(list, "play").handle(context({ song: "x" }).ctx);
  fake.hooks().onEnded(1);
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.deepEqual(sent.at(-1), { eventType: "music.ended", content: "1", channelId: "text1", userId: "u1", guildId: "g1" });
  assert.equal(fake.applied.length, 2, "the next track is played");
  fake.hooks().onGone();
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.equal(sent.at(-1)?.eventType, "music.stop");
});

test("music is refused outside a server", async () => {
  const { ctx, replies } = context({ guildId: undefined, song: "x" });
  await find(commands(fakeVoice("voice1").voice, {}, []), "play").handle(ctx);
  assert.match(replies[0], /only in a server/);
});
