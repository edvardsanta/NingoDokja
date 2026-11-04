import { ApplicationCommandOptionType } from "discord-api-types/v10";
import { randomUUID } from "node:crypto";

import { buildBridgePayload, dispatchToOrchestratorResponse, type OrchestratorReply } from "../transport/bridge.js";
import type { DiscordButtonAction, DiscordButtonContext, DiscordCommandContext, DiscordComponentSpec, DiscordReply, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController, MusicHooks } from "../voice/voice.js";
import { musicReplyText, quiet, say } from "./messages.js";
import { parseComment, parseQueueState, type QueueState } from "./reconcile.js";

type Dispatch = (eventType: string, request: { content: string; userId: string; channelId: string; guildId: string }) => Promise<OrchestratorReply>;

type MusicResult = { text: string; state?: QueueState; presented: boolean };

export type MusicFeature = { commands: DiscordSlashCommand[]; buttons: DiscordButtonAction[] };

// The step the panel's volume buttons take, in percent.
const VOLUME_STEP = 10;
const PANEL_REFRESH_MS = 15_000;

// Music in a voice channel. The orchestrator keeps the queue and its rules; each reply carries the
// queue, which the voice controller plays. Who may control it: anyone in Ningo's voice channel
// while music plays (owner, 2026-10-26); /play from elsewhere is refused. One shared panel stays
// in the channel where the queue began; it is replaced when buried by newer messages.
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
  // The shared panel's channel and last controller: track-ended reports return through this
  // authorized origin. Queue/history reads do not change it.
  const lastCommand = new Map<string, { channelId: string; userId: string }>();
  // The panel of each server: one message, edited as the queue changes.
  const panels = new Map<string, { channelId: string; messageId: string }>();
  const lastState = new Map<string, QueueState>();
  // Ningo's remark on the song each server is playing.
  const comments = new Map<string, { epoch: string; entry: number; text: string }>();

  // Serialize edits and replacements so concurrent replies cannot publish two active panels.
  const panelUpdates = new Map<string, Promise<boolean>>();
  const showPanel = (guildId: string, onlyIfBuried = false): Promise<boolean> => {
    const previous = panelUpdates.get(guildId) ?? Promise.resolve(false);
    const update = previous.catch(() => false).then(async () => {
      if (!voice) return false;
      const state = lastState.get(guildId);
      if (!state || (onlyIfBuried && !state.nowPlaying)) return false;
      const origin = lastCommand.get(guildId);
      const panel = panels.get(guildId);
      const comment = comments.get(guildId);
      const remark = comment?.epoch === state.epoch && comment.entry === state.nowPlaying?.entry ? comment.text : "";
      const message = panelMessage(state, locale, remark);
      try {
        const buried = panel && state.nowPlaying && voice.panelIsLatest && voice.deleteMessage
          ? !(await voice.panelIsLatest(panel)) : false;
        if (panel && !buried) {
          if (onlyIfBuried) return true;
          await voice.editMessage({ ...panel, message });
        } else if (origin && state.nowPlaying) {
          // Publish successfully before retiring the previous controls.
          const messageId = await voice.postMessage({ channelId: origin.channelId, message });
          if (!messageId) return false;
          panels.set(guildId, { channelId: origin.channelId, messageId });
          if (panel && voice.deleteMessage) {
            try {
              await voice.deleteMessage(panel);
            } catch (error) {
              console.error("[dokja-discord] previous music panel cleanup failed", error);
              // If deletion fails, keep the current panel and disable the old controls.
              await voice.editMessage({ ...panel, message: { content: say(locale, "panelMoved"), components: [], embeds: [] } });
            }
          }
        } else {
          return false;
        }
        // An ended queue leaves its last panel without controls.
        if (!state.nowPlaying) panels.delete(guildId);
        return true;
      } catch (error) {
        console.error("[dokja-discord] music panel failed", error);
        return false;
      }
    });
    panelUpdates.set(guildId, update);
    void update.finally(() => { if (panelUpdates.get(guildId) === update) panelUpdates.delete(guildId); });
    return update;
  };

  const panelRefreshes = new Map<string, ReturnType<typeof setTimeout>>();
  const watchPanel = (guildId: string) => {
    if (!voice?.panelIsLatest || !voice.deleteMessage || panelRefreshes.has(guildId)) return;
    const timer = setTimeout(async () => {
      panelRefreshes.delete(guildId);
      if (!lastState.get(guildId)?.nowPlaying) return;
      await showPanel(guildId, true);
      if (lastState.get(guildId)?.nowPlaying) watchPanel(guildId);
    }, PANEL_REFRESH_MS);
    timer.unref();
    panelRefreshes.set(guildId, timer);
  };

  const apply = async (guildId: string, userId: string, music: unknown) => {
    const state = parseQueueState(music);
    if (!state || !voice) return false;
    const previous = lastState.get(guildId);
    // A late reply changes nothing, on the speakers or on the panel.
    if (previous && previous.epoch === state.epoch && state.version < previous.version) return panels.has(guildId);
    if (previous && previous.epoch === state.epoch && state.version === previous.version) return showPanel(guildId);
    lastState.set(guildId, state);
    await voice.applyMusic({ guildId, userId, state, hooks: hooksFor(guildId) });
    const presented = await showPanel(guildId);
    const now = state.nowPlaying;
    if (now) {
      watchPanel(guildId);
    } else {
      clearTimeout(panelRefreshes.get(guildId));
      panelRefreshes.delete(guildId);
    }
    if (now && (previous?.epoch !== state.epoch || previous.nowPlaying?.entry !== now.entry)) askComment(guildId, state.epoch, now.entry);
    return presented;
  };

  // Asks for a remark on the song that just started. It takes as long as the model does, so nothing
  // waits for it, and it reaches the panel only if the same song is still playing.
  const askComment = (guildId: string, epoch: string, entry: number) => {
    const origin = lastCommand.get(guildId);
    if (!origin) return;
    void dispatch("music.comment", { content: `${entry} ${locale}`, guildId, ...origin })
      .then(async (reply) => {
        const text = parseComment(reply.music, entry);
        const state = lastState.get(guildId);
        if (!text || state?.epoch !== epoch || state.nowPlaying?.entry !== entry) return;
        comments.set(guildId, { epoch, entry, text });
        await showPanel(guildId);
      })
      .catch((error) => console.error("[dokja-discord] music comment failed", error));
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

  const run = async (guildId: string, userId: string, channelId: string, eventType: string, content: string): Promise<MusicResult> => {
    // Reads do not relocate the panel. An active queue keeps its original channel;
    // each request still goes through inbound checks for the channel it came from.
    if (eventType !== "music.history" && eventType !== "music.queue") {
      lastCommand.set(guildId, { channelId: panels.get(guildId)?.channelId ?? channelId, userId });
    }
    const reply = await dispatch(eventType, { content, userId, channelId, guildId });
    const text = musicReplyText(locale, eventType, reply.music, reply.reply);
    let presented = false;
    try {
      presented = await apply(guildId, userId, reply.music);
    } catch (error) {
      return { text: `${say(locale, "couldNotPlay")}${error instanceof Error ? ` ${error.message}` : ""}`, presented: false };
    }
    return { text, state: parseQueueState(reply.music), presented };
  };

  const command = (
    name: string,
    description: string,
    eventType: string,
    options: any[] = [],
    content: (context: DiscordCommandContext) => string = () => "",
    needsVoice = true,
    ephemeral = false,
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
      await context.reply(say(locale, eventType === "music.play" ? "looking" : "moment"), ephemeral ? { ephemeral: true } : undefined);
      try {
        const refused = await refusal(context.guildId, context.userId, eventType, needsVoice);
        if (refused) {
          await context.updateReply(refused);
          return;
        }
        const result = await run(context.guildId, context.userId, context.channelId, eventType, content(context));
        if (result.state && result.presented && context.deleteReply) {
          try {
            await context.deleteReply();
            return;
          } catch (error) {
            console.error("[dokja-discord] music acknowledgement cleanup failed", error);
          }
        }
        await context.updateReply(result.text);
      } catch (error) {
        console.error(`[dokja-discord] command ${name} failed`, error);
        await context.updateReply(error instanceof Error ? error.message : say(locale, "failed"));
      }
    },
  });

  // The runtime acknowledges button presses immediately. Success edits the existing
  // panel without another message; refusals and errors remain private.
  const button = (customId: string, label: string, eventType: string, content: (guildId: string) => string = () => "", style: DiscordButtonAction["style"] = "secondary"): DiscordButtonAction => ({
    customId,
    label,
    style,
    defer: true,
    async handle(context: DiscordButtonContext) {
      try {
        const refused = await refusal(context.guildId, context.userId, eventType, true);
        if (refused || !context.guildId) {
          await context.reply({ content: refused ?? say(locale, "onlyServer"), ephemeral: true });
          return;
        }
        const result = await run(context.guildId, context.userId, context.channelId, eventType, content(context.guildId));
        if (!result.state || !result.presented) {
          await context.reply({ content: result.text, ephemeral: true });
          return;
        }

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
      // One person's songs, seen by that person only.
      command("history", "Show the songs you asked for and how they ended.", "music.history", [], () => "", false, true),
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

// What the panel says: the song, who asked for it, the volume, the queue and Ningo's remark, with the controls
// that fit the moment. An ended queue keeps its words and loses its buttons.
export function panelMessage(state: QueueState, locale = "pt-BR", comment = ""): DiscordReply {
  const now = state.nowPlaying;
  if (!now) return { content: say(locale, "queueOver"), components: [], embeds: [] };
  const title = `**${quiet(now.title.trim()) || say(locale, "untitled")}**`;
  const song = now.uploader.trim() ? `${title} – ${quiet(now.uploader.trim())}` : title;
  const details = [
    now.requestedBy ? say(locale, "askedBy", { user: now.requestedBy }) : "",
    `volume ${state.volume}%`,
    state.waiting > 0 ? say(locale, "waiting", { count: state.waiting }) : say(locale, "nothingWaiting"),
  ].filter(Boolean).join(" · ");
  const waiting = state.waitingTracks?.map((track, index) => {
    const title = quiet(track.title.trim()) || say(locale, "untitled");
    const artist = quiet(track.uploader.trim());
    return `${index + 1}. ${title}${artist ? ` – ${artist}` : ""}`;
  }) ?? [];
  const nextUp = waiting.length ? `${say(locale, "nextUp")}\n${waiting.join("\n")}` : "";
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
      description: [song, details, nextUp, comment ? `💬 ${quiet(comment)}` : ""].filter(Boolean).join("\n"),
      ...(now.thumbnail ? { thumbnailUrl: now.thumbnail } : {}),
    }],
    components,
  };
}

// The commands alone, for callers that do not show buttons.
export function createMusicCommands(params: Parameters<typeof createMusic>[0]): DiscordSlashCommand[] {
  return createMusic(params).commands;
}
