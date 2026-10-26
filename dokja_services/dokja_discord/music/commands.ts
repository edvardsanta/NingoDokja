import { ApplicationCommandOptionType } from "discord-api-types/v10";
import { randomUUID } from "node:crypto";

import { buildBridgePayload, dispatchToOrchestratorResponse, type OrchestratorReply } from "../transport/bridge.js";
import type { DiscordCommandContext, DiscordSlashCommand } from "../types.js";
import type { DiscordVoiceController, MusicHooks } from "../voice/voice.js";
import { parseQueueState } from "./reconcile.js";

type Dispatch = (eventType: string, request: { content: string; userId: string; channelId: string; guildId: string }) => Promise<OrchestratorReply>;

// Music in a voice channel. The orchestrator keeps the queue and its rules; each reply carries the
// queue, which the voice controller plays. Who may control it: anyone in Ningo's voice channel
// while music plays (owner, 2026-10-26); /play from elsewhere is refused.
export function createMusicCommands(params: {
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  voice?: DiscordVoiceController;
  dispatch?: Dispatch;
}): DiscordSlashCommand[] {
  const { voice } = params;
  const dispatch: Dispatch = params.dispatch ?? ((eventType, request) =>
    dispatchToOrchestratorResponse(
      params.orchestratorEndpoint,
      params.orchestratorTimeoutMs,
      buildBridgePayload({ eventType, ...request, messageId: randomUUID(), entryKind: "command" }),
    ));
  // Where a server's last music command came from: track-ended reports go back through it, so they
  // pass the same inbound checks (the server and its allowed channels).
  const lastCommand = new Map<string, { channelId: string; userId: string }>();

  const apply = async (guildId: string, userId: string, music: unknown) => {
    const state = parseQueueState(music);
    if (!state || !voice) return;
    await voice.applyMusic({ guildId, userId, state, hooks: hooksFor(guildId) });
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
      const guildId = context.guildId;
      if (!guildId) {
        await context.reply("Music plays only in a server's voice channel.", { ephemeral: true });
        return;
      }
      if (!voice) {
        await context.reply("Voice controller is not configured.", { ephemeral: true });
        return;
      }
      // Answer at once: Discord drops a command not answered within three seconds, and a search
      // can take longer.
      await context.reply(eventType === "music.play" ? "Looking for it..." : "One moment...");
      try {
        if (needsVoice) {
          const playing = voice.musicChannel({ guildId });
          const member = await voice.memberVoiceChannel({ guildId, userId: context.userId });
          if (eventType === "music.play" && !member) {
            await context.updateReply("Join a voice channel to play music.");
            return;
          }
          if (playing && member !== playing) {
            await context.updateReply(`Music is playing in <#${playing}>. Join it to control the queue.`);
            return;
          }
        }
        lastCommand.set(guildId, { channelId: context.channelId, userId: context.userId });
        const reply = await dispatch(eventType, { content: content(context), userId: context.userId, channelId: context.channelId, guildId });
        try {
          await apply(guildId, context.userId, reply.music);
        } catch (error) {
          await context.updateReply(`${reply.reply}\n${error instanceof Error ? error.message : "Could not play it."}`);
          return;
        }
        await context.updateReply(reply.reply || "Done.");
      } catch (error) {
        console.error(`[dokja-discord] command ${name} failed`, error);
        await context.updateReply(error instanceof Error ? error.message : "That could not be done.");
      }
    },
  });

  return [
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
  ];
}
