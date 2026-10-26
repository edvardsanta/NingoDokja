import assert from "node:assert/strict";
import test from "node:test";

import { PANEL, createMusic, createMusicCommands, panelMessage } from "../music/commands.js";
import type { DiscordCommandContext } from "../types.js";

type Sent = { eventType: string; content: string; channelId: string; guildId: string; userId: string };

// A voice controller that records what it is asked to play, with the person's channel and the
// channel Ningo plays in set by the test.
function fakeVoice(member: string | undefined, playing?: string) {
  const applied: unknown[] = [];
  const posted: Array<{ channelId: string; message: any }> = [];
  const edited: Array<{ channelId: string; messageId: string; message: any }> = [];
  let hooks: { onEnded(entry: number): void; onGone(): void } | undefined;
  const voice = {
    async postMessage(args: { channelId: string; message: unknown }) { posted.push(args as never); return `panel${posted.length}`; },
    async editMessage(args: { channelId: string; messageId: string; message: unknown }) { edited.push(args as never); },
    async memberVoiceChannel() { return member; },
    musicChannel() { return playing; },
    async applyMusic(args: { state: unknown; hooks: typeof hooks }) {
      applied.push(args.state);
      hooks = args.hooks;
    },
  };
  return { voice: voice as never, applied, posted, edited, hooks: () => hooks! };
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
  ...(entry ? {
    now_playing: { entry, url: `https://play.example/${entry}`, title: "Monalisa", uploader: "Jorge" },
    added: { entry, title: "Monalisa", uploader: "Jorge" }, place: 0,
  } : {}),
});

function commands(voice: unknown, replies: Record<string, { reply: string; music?: unknown }>, sent: Sent[]) {
  return createMusicCommands({
    orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: voice as never, locale: "en",
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

test("the panel is posted with the first song, edited as the queue changes, and loses its buttons at the end", async () => {
  const sent: Sent[] = [];
  const fake = fakeVoice("voice1", undefined);
  const music = createMusic({
    orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: fake.voice, locale: "en",
    dispatch: async (eventType, request) => {
      sent.push({ eventType, ...request });
      const version = sent.length;
      if (eventType === "music.stop") return { reply: "Stopped.", music: queue(version) };
      if (eventType === "music.pause") return { reply: "Paused.", music: { ...queue(version, 1), paused: true } };
      return { reply: "Now playing.", music: queue(version, 1) };
    },
  });
  const play = music.commands.find((entry) => entry.name === "play")!;
  await play.handle(context({ song: "x" }).ctx);
  assert.equal(fake.posted.length, 1);
  assert.equal(fake.posted[0].channelId, "text1");
  assert.match(fake.posted[0].message.content, /Now playing: \*\*Monalisa\*\* – Jorge/);

  const pause = music.buttons.find((entry) => entry.customId === PANEL.pause)!;
  const pressed: string[] = [];
  await pause.handle({ customId: PANEL.pause, userId: "u1", channelId: "text1", messageId: "panel1", guildId: "g1",
    reply: async (message: any) => { pressed.push(message.content); }, update: async () => {}, showModal: async () => {} });
  assert.deepEqual(pressed, ["Paused."], "the button answers only the person who pressed it");
  assert.equal(fake.posted.length, 1, "the panel is edited, not posted again");
  assert.match(fake.edited.at(-1)!.message.content, /Paused/);
  assert.ok(fake.edited.at(-1)!.message.components.some((button: any) => button.customId === PANEL.resume));

  await music.commands.find((entry) => entry.name === "stop")!.handle(context().ctx);
  assert.deepEqual(fake.edited.at(-1)!.message.components, []);
  assert.match(fake.edited.at(-1)!.message.content, /queue is over/);
});

test("a panel button follows the commands' rule: Ningo's voice channel while music plays", async () => {
  const sent: Sent[] = [];
  const music = createMusic({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: fakeVoice("voice2", "voice1").voice, locale: "en",
    dispatch: async (eventType, request) => { sent.push({ eventType, ...request }); return { reply: "" }; } });
  const said: string[] = [];
  await music.buttons.find((entry) => entry.customId === PANEL.skip)!.handle({ customId: PANEL.skip, userId: "u2", channelId: "text1", messageId: "p", guildId: "g1",
    reply: async (message: any) => { said.push(message.content); }, update: async () => {}, showModal: async () => {} });
  assert.match(said[0], /Join it to control the queue/);
  assert.deepEqual(sent, []);
});

test("the volume buttons step from the last volume and stay within 0 to 100", async () => {
  const sent: Sent[] = [];
  const music = createMusic({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: fakeVoice("voice1").voice, locale: "en",
    dispatch: async (eventType, request) => { sent.push({ eventType, ...request }); return { reply: "", music: { ...queue(sent.length, 1), volume: 95 } }; } });
  await music.commands.find((entry) => entry.name === "play")!.handle(context({ song: "x" }).ctx);
  const press = (customId: string) => music.buttons.find((entry) => entry.customId === customId)!.handle({ customId, userId: "u1", channelId: "text1", messageId: "p", guildId: "g1",
    reply: async () => {}, update: async () => {}, showModal: async () => {} });
  await press(PANEL.louder);
  assert.equal(sent.at(-1)?.content, "100", "95 + 10 stops at 100");
  await press(PANEL.quieter);
  assert.equal(sent.at(-1)?.content, "85");
});

test("the panel says who asked, the volume and what waits", () => {
  const state = { epoch: "e", version: 1, paused: false, volume: 60, waiting: 2,
    nowPlaying: { entry: 1, url: "https://play.example/1", title: "Monalisa", uploader: "Jorge", requestedBy: "42" } };
  const message = panelMessage(state, "en") as any;
  assert.equal((panelMessage(state) as any).content, "🎶 Tocando agora: **Monalisa** – Jorge\npedido por <@42> · volume 60% · 2 na fila",
    "Portuguese by default, as DOKJA_DISCORD_LOCALE");
  assert.equal(message.content, "🎶 Now playing: **Monalisa** – Jorge\nasked by <@42> · volume 60% · 2 waiting");
  assert.deepEqual(message.components.map((button: any) => button.customId), [PANEL.pause, PANEL.skip, PANEL.stop, PANEL.quieter, PANEL.louder]);
});

test("music answers in the configured language, refusals included", async () => {
  const sent: Sent[] = [];
  const reply = (music: unknown) => createMusic({ orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: fakeVoice("voice1").voice,
    dispatch: async (eventType, request) => { sent.push({ eventType, ...request }); return { reply: "English from the orchestrator", music }; } });
  for (const [music, expected] of [
    [{ ok: false, code: "queue_full", reason: "the queue is full" }, "A fila está cheia: espere uma música acabar."],
    [{ ok: false, code: "track_refused", reason: "only track links are supported" }, "Não dá para tocar isso: only track links are supported"],
    [{ ok: false, code: "something_new", reason: "a reason" }, "a reason"],
    [{ ...queue(1, 1), place: 2 }, "Na fila (#2): Monalisa – Jorge."],
  ] as const) {
    const { ctx, replies } = context({ song: "x" });
    await reply(music).commands.find((entry) => entry.name === "play")!.handle(ctx);
    assert.equal(replies[0], "Procurando...");
    assert.equal(replies.at(-1), expected);
  }
});
