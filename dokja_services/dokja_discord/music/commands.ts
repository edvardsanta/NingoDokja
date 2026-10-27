import { ApplicationCommandOptionType } from "discord-api-types/v10";
import { randomUUID } from "node:crypto";

import { buildBridgePayload, dispatchToOrchestratorResponse, type OrchestratorReply } from "../transport/bridge.js";
import type { DiscordButtonAction, DiscordButtonContext, DiscordCommandContext, DiscordComponentSpec, DiscordReply, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController, MusicHooks } from "../voice/voice.js";
import { musicReplyText, quiet, say } from "./messages.js";
import { parseQueueState, type QueueState } from "./reconcile.js";

type Dispatch = (eventType: string, request: { content: string; userId: string; channelId: string; guildId: string }) => Promise<OrchestratorReply>;

export type MusicFeature = { commands: DiscordSlashCommand[]; buttons: DiscordButtonAction[] };

// The step the panel's volume buttons take, in percent.
const VOLUME_STEP = 10;

// Music in a voice channel. The orchestrator keeps the queue and its rules; each reply carries the
// queue, which the voice controller plays. Who may control it: anyone in Ningo's voice channel
// while music plays (owner, 2026-10-26); /play from elsewhere is refused. A panel in the channel of
// the last command shows what plays, with buttons that work like the commands.
export function createMusic(params: {
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  voice?: DiscordVoiceController;
  dispatch?: Dispatch;
  // the language people are answered in (DOKJA_DISCORD_LOCALE)
  locale?: string;
}): MusicFeature {
  const { voice } = params;
  const locale = params.locale ?? "pt-BR";
  const dispatch: Dispatch = params.dispatch ?? ((eventType, request) =>
    dispatchToOrchestratorResponse(
      params.orchestratorEndpoint,
      params.orchestratorTimeoutMs,
      buildBridgePayload({ eventType, ...request, messageId: randomUUID(), entryKind: "command" }),
    ));
  // Where a server's last music command came from: track-ended reports go back through it, so they
  // pass the same inbound checks (the server and its allowed channels), and the panel lives there.
  const lastCommand = new Map<string, { channelId: string; userId: string }>();
  // The panel of each server: one message, edited as the queue changes.
  const panels = new Map<string, { channelId: string; messageId: string }>();
  const lastState = new Map<string, QueueState>();

  const showPanel = async (guildId: string, state: QueueState) => {
    if (!voice) return;
    const origin = lastCommand.get(guildId);
    const panel = panels.get(guildId);
    const message = panelMessage(state, locale);
    try {
      if (panel && (!origin || panel.channelId === origin.channelId)) {
        await voice.editMessage({ ...panel, message });
      } else if (origin && state.nowPlaying) {
        const messageId = await voice.postMessage({ channelId: origin.channelId, message });
        if (messageId) panels.set(guildId, { channelId: origin.channelId, messageId });
      }
      // An ended queue leaves its last panel without buttons; the next song posts a new one.
      if (!state.nowPlaying) panels.delete(guildId);
    } catch (error) {
      console.error("[dokja-discord] music panel failed", error);
    }
  };

  const apply = async (guildId: string, userId: string, music: unknown) => {
    const state = parseQueueState(music);
    if (!state || !voice) return;
    const previous = lastState.get(guildId);
    // A late reply changes nothing, on the speakers or on the panel.
    if (previous && previous.epoch === state.epoch && state.version <= previous.version) return;
    lastState.set(guildId, state);
    await voice.applyMusic({ guildId, userId, state, hooks: hooksFor(guildId) });
    await showPanel(guildId, state);
  };

  // A report the player makes on its own: the track ended (or could not start), or the voice
  // connection is gone.
  const report = (guildId: string, eventType: "music.ended" | "music.stop", content: string) => {
    const origin = lastCommand.get(guildId);
    if (!origin) return;
    void dispatch(eventType, { content, guildId, ...origin })
      .then((reply) => apply(guildId, origin.userId, reply.music))
      .catch((error) => console.error(`[dokja-discord] ${eventType} report failed`, error));
  };
  const hooksFor = (guildId: string): MusicHooks => ({
    onEnded: (entry) => report(guildId, "music.ended", String(entry)),
    onGone: () => report(guildId, "music.stop", ""),
  });

  // The rule every control follows, from a command or a button: a server, and the person in
  // Ningo's voice channel while music plays. Undefined when allowed, otherwise what to say.
  const refusal = async (guildId: string | undefined, userId: string, eventType: string, needsVoice: boolean) => {
    if (!guildId) return say(locale, "onlyServer");
    if (!voice) return say(locale, "noVoice");
    if (!needsVoice) return undefined;
    const playing = voice.musicChannel({ guildId });
    const member = await voice.memberVoiceChannel({ guildId, userId });
    if (eventType === "music.play" && !member) return say(locale, "joinVoice");
    if (playing && member !== playing) return say(locale, "playingElsewhere", { channel: playing });
    return undefined;
  };

  const run = async (guildId: string, userId: string, channelId: string, eventType: string, content: string) => {
    lastCommand.set(guildId, { channelId, userId });
    const reply = await dispatch(eventType, { content, userId, channelId, guildId });
    const text = musicReplyText(locale, eventType, reply.music, reply.reply);
    try {
      await apply(guildId, userId, reply.music);
    } catch (error) {
      return `${text}\n${error instanceof Error ? error.message : say(locale, "couldNotPlay")}`;
    }
    return text;
  };

  const command = (
    name: string,
    description: string,
    eventType: string,
    options: any[] = [],
    content: (context: DiscordCommandContext) => string = () => "",
    needsVoice = true,
  ): DiscordSlashCommand => ({
    name,
    description,
    defer: false,
    options,
    async handle(context) {
      if (!context.guildId || !voice) {
        await context.reply((await refusal(context.guildId, context.userId, eventType, false))!, { ephemeral: true });
        return;
      }
      // Answer at once: Discord drops a command not answered within three seconds, and a search
      // can take longer.
      await context.reply(say(locale, eventType === "music.play" ? "looking" : "moment"));
      try {
        const refused = await refusal(context.guildId, context.userId, eventType, needsVoice);
        if (refused) {
          await context.updateReply(refused);
          return;
        }
        await context.updateReply(await run(context.guildId, context.userId, context.channelId, eventType, content(context)));
      } catch (error) {
        console.error(`[dokja-discord] command ${name} failed`, error);
        await context.updateReply(error instanceof Error ? error.message : say(locale, "failed"));
      }
    },
  });

  // The panel's buttons do what the commands do, answering only the person who pressed.
  const button = (customId: string, label: string, eventType: string, content: (guildId: string) => string = () => "", style: DiscordButtonAction["style"] = "secondary"): DiscordButtonAction => ({
    customId,
    label,
    style,
    async handle(context: DiscordButtonContext) {
      try {
        const refused = await refusal(context.guildId, context.userId, eventType, true);
        if (refused || !context.guildId) {
          await context.reply({ content: refused ?? say(locale, "onlyServer"), ephemeral: true });
          return;
        }
        const text = await run(context.guildId, context.userId, context.channelId, eventType, content(context.guildId));
        await context.reply({ content: text, ephemeral: true });
      } catch (error) {
        console.error(`[dokja-discord] button ${customId} failed`, error);
        await context.reply({ content: error instanceof Error ? error.message : say(locale, "failed"), ephemeral: true });
      }
    },
  });
  const volumeBy = (step: number) => (guildId: string) =>
    String(Math.min(100, Math.max(0, (lastState.get(guildId)?.volume ?? 100) + step)));

  return {
    commands: [
      command("play", "Play a song in your voice channel: a few words or a link.", "music.play", [
        { name: "song", description: "A few words (song and artist) or a link", type: ApplicationCommandOptionType.String, required: true },
      ], (context) => context.getString("song")),
      command("skip", "Skip the song playing.", "music.skip"),
      command("pause", "Pause the music.", "music.pause"),
      command("resume", "Resume the music.", "music.resume"),
      command("stop", "Stop the music and empty the queue.", "music.stop"),
      command("queue", "Show the song playing and the ones waiting.", "music.queue", [], () => "", false),
      command("volume", "Set the music volume, from 0 to 100.", "music.volume", [
        { name: "percent", description: "0 to 100", type: ApplicationCommandOptionType.Integer, required: true, min_value: 0, max_value: 100 },
      ], (context) => String(context.getNumber("percent") ?? "")),
    ],
    buttons: [
      button(PANEL.pause, say(locale, "pause"), "music.pause"),
      button(PANEL.resume, say(locale, "resume"), "music.resume", undefined, "primary"),
      button(PANEL.skip, say(locale, "skip"), "music.skip"),
      button(PANEL.stop, say(locale, "stop"), "music.stop", undefined, "danger"),
      button(PANEL.quieter, `🔉 −${VOLUME_STEP}`, "music.volume", volumeBy(-VOLUME_STEP)),
      button(PANEL.louder, `🔊 +${VOLUME_STEP}`, "music.volume", volumeBy(VOLUME_STEP)),
    ],
  };
}

export const PANEL = {
  pause: "music_pause_button",
  resume: "music_resume_button",
  skip: "music_skip_button",
  stop: "music_stop_button",
  quieter: "music_quieter_button",
  louder: "music_louder_button",
} as const;

// What the panel says: the song, who asked for it, the volume and the queue, with the controls
// that fit the moment. An ended queue keeps its words and loses its buttons.
export function panelMessage(state: QueueState, locale = "pt-BR"): DiscordReply {
  const now = state.nowPlaying;
  if (!now) return { content: say(locale, "queueOver"), components: [], embeds: [] };
  const title = `**${quiet(now.title.trim()) || say(locale, "untitled")}**`;
  const song = now.uploader.trim() ? `${title} – ${quiet(now.uploader.trim())}` : title;
  const details = [
    now.requestedBy ? say(locale, "askedBy", { user: now.requestedBy }) : "",
    `volume ${state.volume}%`,
    state.waiting > 0 ? say(locale, "waiting", { count: state.waiting }) : say(locale, "nothingWaiting"),
  ].filter(Boolean).join(" · ");
  const components: DiscordComponentSpec[] = [
    state.paused
      ? { kind: "button", customId: PANEL.resume, label: say(locale, "resume"), style: "primary" }
      : { kind: "button", customId: PANEL.pause, label: say(locale, "pause"), style: "secondary" },
    { kind: "button", customId: PANEL.skip, label: say(locale, "skip"), style: "secondary" },
    { kind: "button", customId: PANEL.stop, label: say(locale, "stop"), style: "danger" },
    { kind: "button", customId: PANEL.quieter, label: `🔉 −${VOLUME_STEP}`, style: "secondary" },
    { kind: "button", customId: PANEL.louder, label: `🔊 +${VOLUME_STEP}`, style: "secondary" },
  ];
  // A card with the song, who asked and the queue, and the cover beside it when the track has one.
  return {
    content: "",
    embeds: [{
      title: say(locale, state.paused ? "panelPaused" : "panelPlaying"),
      description: `${song}\n${details}`,
      ...(now.thumbnail ? { thumbnailUrl: now.thumbnail } : {}),
    }],
    components,
  };
}

// The commands alone, for callers that do not show buttons.
export function createMusicCommands(params: Parameters<typeof createMusic>[0]): DiscordSlashCommand[] {
  return createMusic(params).commands;
}
