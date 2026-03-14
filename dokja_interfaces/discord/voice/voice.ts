import {
  AudioPlayerStatus,
  NoSubscriberBehavior,
  VoiceConnectionStatus,
  createAudioPlayer,
  entersState,
  joinVoiceChannel,
} from "@discordjs/voice";
import {
  ChannelType,
  Client as DiscordJsClient,
  GatewayIntentBits,
  type GuildMember,
} from "discord.js";

import { createRadioStreamSession, type RadioStreamSession } from "./radio_stream.js";

type VoiceSession = {
  connection: ReturnType<typeof joinVoiceChannel>;
  player: ReturnType<typeof createAudioPlayer>;
  stream: RadioStreamSession;
};

export type DiscordVoiceController = {
  attachClient(client?: unknown, botUserId?: string): void;
  playRadio(params: { guildId?: string; userId: string; streamUrl: string }): Promise<string>;
  stopRadio(params: { guildId?: string }): Promise<string>;
};

export function createDiscordVoiceController(options: {
  token: string;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
  ffmpegPath?: string;
  readyTimeoutMs?: number;
}): DiscordVoiceController {
  const sessions = new Map<string, VoiceSession>();
  const ffmpegPath = options.ffmpegPath ?? "ffmpeg";
  const readyTimeoutMs = options.readyTimeoutMs ?? 30_000;

  let voiceClient: DiscordJsClient | undefined;
  let voiceClientReady: Promise<DiscordJsClient> | undefined;

  const ensureVoiceClient = async (): Promise<DiscordJsClient> => {
    if (voiceClientReady) {
      return await voiceClientReady;
    }

    const client = new DiscordJsClient({
      intents: [GatewayIntentBits.Guilds, GatewayIntentBits.GuildVoiceStates],
    });

    client.on("clientReady", () => {
      options.log?.(
        "[dokja-discord] discord.js voice client ready",
        JSON.stringify({ userId: client.user?.id }),
      );
    });

    client.on("raw", (packet) => {
      if (packet.t === "VOICE_STATE_UPDATE" || packet.t === "VOICE_SERVER_UPDATE") {
        options.log?.(
          "[dokja-discord] discord.js raw voice event",
          JSON.stringify({
            type: packet.t,
            data: packet.d,
          }),
        );
      }
    });

    client.on("error", (error) => {
      options.error?.("[dokja-discord] discord.js voice client error", error);
    });

    voiceClientReady = (async () => {
      await client.login(options.token);
      voiceClient = client;
      return client;
    })();

    return await voiceClientReady;
  };

  return {
    attachClient(_client?: unknown) {
      void ensureVoiceClient();
    },
    async playRadio({ guildId, userId, streamUrl }) {
      if (!guildId) {
        throw new Error("Radio playback only works in guild voice channels");
      }

      const client = await ensureVoiceClient();
      const member = await fetchVoiceMember(client, guildId, userId);
      const voiceChannel = member.voice.channel;

      if (!voiceChannel) {
        throw new Error("Join a voice channel before starting the radio");
      }
      if (
        voiceChannel.type !== ChannelType.GuildVoice &&
        voiceChannel.type !== ChannelType.GuildStageVoice
      ) {
        throw new Error("Radio playback only works in voice channels");
      }

      stopGuildSession(sessions.get(guildId));

      const connection = joinVoiceChannel({
        channelId: voiceChannel.id,
        guildId,
        adapterCreator: voiceChannel.guild.voiceAdapterCreator,
        selfDeaf: false,
      });

      connection.on("debug", (message) => {
        options.log?.(
          "[dokja-discord] discord.js voice debug",
          JSON.stringify({ guildId, channelId: voiceChannel.id, message }),
        );
      });
      connection.on("stateChange", (_oldState, newState) => {
        options.log?.(
          "[dokja-discord] voice connection state",
          JSON.stringify({ guildId, voiceChannelId: voiceChannel.id, status: newState.status }),
        );
      });

      try {
        await entersState(connection, VoiceConnectionStatus.Ready, readyTimeoutMs);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        options.error?.(
          "[dokja-discord] voice ready wait failed",
          JSON.stringify({
            guildId,
            voiceChannelId: voiceChannel.id,
            status: connection.state.status,
            readyTimeoutMs,
            error: message,
          }),
        );
        connection.destroy();
        throw new Error(
          `Joined <#${voiceChannel.id}>, but the Discord voice transport never became ready. Check bot voice permissions and host networking and try again.`,
        );
      }

      const player = createAudioPlayer({
        behaviors: { noSubscriber: NoSubscriberBehavior.Play },
      });
      const stream = createRadioStreamSession(streamUrl, {
        ffmpegPath,
        log: options.log,
        error: options.error,
      });

      player.on("error", (error) => {
        options.error?.("[dokja-discord] radio player error", error);
      });
      player.on("stateChange", (_oldState, newState) => {
        options.log?.(
          "[dokja-discord] radio player state",
          JSON.stringify({ guildId, status: newState.status }),
        );
      });
      player.on(AudioPlayerStatus.Idle, () => {
        options.log?.("[dokja-discord] radio player idle", JSON.stringify({ guildId, streamUrl }));
      });

      connection.subscribe(player);
      player.play(stream.resource);

      try {
        await entersState(player, AudioPlayerStatus.Playing, 15_000);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        options.error?.(
          "[dokja-discord] radio player failed to start",
          JSON.stringify({
            guildId,
            voiceChannelId: voiceChannel.id,
            streamUrl,
            connectionStatus: connection.state.status,
            playerStatus: player.state.status,
            error: message,
          }),
        );
        stream.stop();
        player.stop(true);
        connection.destroy();
        throw new Error("Joined the voice channel, but the radio stream did not start playing.");
      }

      sessions.set(guildId, { connection, player, stream });
      options.log?.(
        "[dokja-discord] radio started",
        JSON.stringify({ guildId, voiceChannelId: voiceChannel.id, streamUrl }),
      );
      return `Playing radio in <#${voiceChannel.id}>`;
    },
    async stopRadio({ guildId }) {
      if (!guildId) {
        throw new Error("Radio stop only works in guild voice channels");
      }
      const session = sessions.get(guildId);
      if (!session) {
        return "No radio is playing in this server.";
      }
      stopGuildSession(session);
      sessions.delete(guildId);
      options.log?.("[dokja-discord] radio stopped", JSON.stringify({ guildId }));
      return "Radio stopped.";
    },
  };
}

async function fetchVoiceMember(
  client: DiscordJsClient,
  guildId: string,
  userId: string,
): Promise<GuildMember> {
  const guild = client.guilds.cache.get(guildId) ?? (await client.guilds.fetch(guildId));
  const member = guild.members.cache.get(userId) ?? (await guild.members.fetch(userId));
  return member;
}

function stopGuildSession(session: VoiceSession | undefined) {
  if (!session) {
    return;
  }
  session.player.stop(true);
  session.stream.stop();
  session.connection.destroy();
}
