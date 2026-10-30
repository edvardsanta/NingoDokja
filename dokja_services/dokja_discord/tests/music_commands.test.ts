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
  assert.deepEqual(sent.filter((event) => event.eventType !== "music.comment"), [{ eventType: "music.play", content: "monalisa jorge", userId: "u1", channelId: "text1", guildId: "g1" }]);
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
  assert.deepEqual(sent.find((event) => event.eventType === "music.ended"), { eventType: "music.ended", content: "1", channelId: "text1", userId: "u1", guildId: "g1" });
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
  assert.equal(fake.posted[0].message.embeds[0].title, "🎶 Now playing");
  assert.match(fake.posted[0].message.embeds[0].description, /\*\*Monalisa\*\* – Jorge/);

  const pause = music.buttons.find((entry) => entry.customId === PANEL.pause)!;
  const pressed: string[] = [];
  await pause.handle({ customId: PANEL.pause, userId: "u1", channelId: "text1", messageId: "panel1", guildId: "g1",
    reply: async (message: any) => { pressed.push(message.content); }, update: async () => {}, showModal: async () => {} });
  assert.deepEqual(pressed, ["Paused."], "the button answers only the person who pressed it");
  assert.equal(fake.posted.length, 1, "the panel is edited, not posted again");
  assert.match(fake.edited.at(-1)!.message.embeds[0].title, /Paused/);
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
    nowPlaying: { entry: 1, url: "https://play.example/1", title: "Monalisa", uploader: "Jorge", requestedBy: "42", thumbnail: "" } };
  const message = panelMessage(state, "en") as any;
  const portuguese = (panelMessage(state) as any).embeds[0];
  assert.equal(portuguese.title, "🎶 Tocando agora", "Portuguese by default, as DOKJA_DISCORD_LOCALE");
  assert.equal(portuguese.description, "**Monalisa** – Jorge\npedido por <@42> · volume 60% · 2 na fila");
  assert.equal(message.embeds[0].thumbnailUrl, undefined, "no cover, no picture");
  const covered = panelMessage({ ...state, nowPlaying: { ...state.nowPlaying, thumbnail: "https://covers.example/album.jpg" } }, "en") as any;
  assert.equal(covered.embeds[0].thumbnailUrl, "https://covers.example/album.jpg");
  assert.equal(message.embeds[0].description, "**Monalisa** – Jorge\nasked by <@42> · volume 60% · 2 waiting");
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

test("a title from outside never pings the server", () => {
  const message = panelMessage({ epoch: "e", version: 1, paused: false, volume: 100, waiting: 0,
    nowPlaying: { entry: 1, url: "https://play.example/1", title: "@everyone wake up", uploader: "@here", requestedBy: "", thumbnail: "" } }, "en") as any;
  assert.doesNotMatch(message.embeds[0].description, /@everyone|@here/);
  assert.match(message.embeds[0].description, /@\u200beveryone wake up/);
});

test("the queue list never pings either", async () => {
  const { musicReplyText } = await import("../music/messages.js");
  const text = musicReplyText("en", "music.queue", { ok: true, now_playing: { title: "ok" }, waiting: [{ title: "@here now" }] }, "");
  assert.doesNotMatch(text, /@here/);
});


test("an empty orchestrator response never confirms a music command or starts playback", async () => {
  for (const locale of ["en", "pt-BR"]) {
    const { voice, applied, posted } = fakeVoice("voice1");
    const feature = createMusic({
      orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000,
      voice, locale, dispatch: async () => ({ reply: "" }),
    });
    const { ctx, replies } = context({ song: "a song" });
    await feature.commands.find((entry) => entry.name === "play")!.handle(ctx);
    assert.equal(replies.at(-1), locale === "en"
      ? "The music command could not be confirmed."
      : "Não foi possível confirmar o comando de música.");
    assert.equal(applied.length, 0);
    assert.equal(posted.length, 0);
  }
});

test("a missing music result with whitespace-only text is unconfirmed", async () => {
  const { musicReplyText } = await import("../music/messages.js");
  assert.equal(musicReplyText("en", "music.play", undefined, "  "), "The music command could not be confirmed.");
  assert.equal(musicReplyText("en", "music.play", undefined, "Music is switched off."), "Music is switched off.");
});


test("failed voice readiness does not claim playback or publish a playing panel", async () => {
  const fake = fakeVoice("voice1");
  fake.voice.applyMusic = async () => { throw new Error("Voice transport did not become ready."); };
  const list = commands(fake.voice, { "music.play": { reply: "Now playing.", music: queue(1, 1) } }, []);
  const { ctx, replies } = context({ song: "x" });
  await find(list, "play").handle(ctx);
  assert.match(replies.at(-1)!, /Could not play it/);
  assert.doesNotMatch(replies.at(-1)!, /Now playing/);
  assert.equal(fake.posted.length, 0);
});

const playingEntry = (entry: number, version: number) => ({
  ok: true, epoch: "e1", version, paused: false, volume: 100, waiting: [],
  now_playing: { entry, url: `https://play.example/${entry}`, title: `Song ${entry}` },
});

test("a remark on the song reaches the panel without holding anything", async () => {
  const fake = fakeVoice("voice1");
  const sent: Sent[] = [];
  const { ctx } = context();
  let answer: (reply: { reply: string; music?: unknown }) => void = () => {};
  const remark = new Promise<{ reply: string; music?: unknown }>((resolve) => { answer = resolve; });
  const list = createMusicCommands({
    orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: fake.voice as never, locale: "en",
    dispatch: async (eventType, request) => {
      sent.push({ eventType, ...request });
      return eventType === "music.comment" ? remark : { reply: "", music: playingEntry(1, 1) };
    },
  });
  await find(list, "play").handle(ctx);
  assert.equal(sent.at(-1)?.eventType, "music.comment");
  assert.equal(sent.at(-1)?.content, "1 en", "the entry and the people's language");
  assert.doesNotMatch(fake.posted[0].message.embeds[0].description, /💬/, "the panel does not wait for the model");
  answer({ reply: "", music: { ok: true, entry: 1, comment: "@everyone, a classic." } });
  await new Promise((resolve) => setImmediate(resolve));
  assert.match(fake.edited.at(-1)!.message.embeds[0].description, /💬 @\u200beveryone, a classic\./);
});

test("a remark on a song that already ended is dropped", async () => {
  const fake = fakeVoice("voice1");
  const { ctx } = context();
  let answer: (reply: { reply: string; music?: unknown }) => void = () => {};
  const remark = new Promise<{ reply: string; music?: unknown }>((resolve) => { answer = resolve; });
  let plays = 0;
  const list = createMusicCommands({
    orchestratorEndpoint: "http://orch", orchestratorTimeoutMs: 1000, voice: fake.voice as never, locale: "en",
    dispatch: async (eventType) => {
      if (eventType === "music.comment") return remark;
      plays += 1;
      return { reply: "", music: playingEntry(plays, plays) };
    },
  });
  await find(list, "play").handle(ctx);
  await find(list, "skip").handle(ctx);
  const edits = fake.edited.length;
  answer({ reply: "", music: { ok: true, entry: 1, comment: "too late" } });
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(fake.edited.length, edits, "the panel of the next song is left alone");
});

test("history is shown only to who asked, and leaves the panel where it is", async () => {
  const fake = fakeVoice(undefined);
  const sent: Sent[] = [];
  const history = { ok: true, history: [
    { song: "Monalisa – Jorge", ended: "finished" },
    { song: "@everyone wake up", ended: "skipped" },
  ] };
  const list = commands(fake.voice, { "music.history": { reply: "", music: history } }, sent);
  const options: unknown[] = [];
  const { ctx, replies } = context({ reply: async (_message: unknown, option: unknown) => { options.push(option); } } as never);
  await find(list, "history").handle(ctx);
  assert.deepEqual(options, [{ ephemeral: true }]);
  assert.equal(sent[0].eventType, "music.history");
  assert.equal(sent[0].content, "", "the person is the verified sender, nothing typed");
  assert.equal(replies.at(-1), "Your latest songs:\n- Monalisa – Jorge (heard to the end)\n- @\u200beveryone wake up (skipped)");
});

test("history says so when there is nothing, or when memory cannot answer", async () => {
  const empty = commands(fakeVoice(undefined).voice, { "music.history": { reply: "", music: { ok: true, history: [] } } }, []);
  const first = context();
  await find(empty, "history").handle(first.ctx);
  assert.equal(first.replies.at(-1), "You have not asked for any song yet.");
  const down = commands(fakeVoice(undefined).voice, { "music.history": { reply: "", music: { ok: false, code: "no_memory", reason: "x" } } }, []);
  const second = context();
  await find(down, "history").handle(second.ctx);
  assert.equal(second.replies.at(-1), "The music memory is not available right now.");
});
