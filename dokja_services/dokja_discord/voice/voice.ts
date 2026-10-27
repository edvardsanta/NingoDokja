import {
  AudioPlayerStatus,
  EndBehaviorType,
  NoSubscriberBehavior,
  VoiceConnectionStatus,
  createAudioPlayer,
  entersState,
  joinVoiceChannel,
} from "@discordjs/voice";
import { Buffer } from "node:buffer";
import { mkdtempSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  ChannelType,
  Client as DiscordJsClient,
  GatewayIntentBits,
  type GuildBasedChannel,
  type GuildMember,
} from "discord.js";

import { IDLE, reconcile, type LocalPlayback, type QueueState } from "../music/reconcile.js";
import {
  createFileAudioSession,
  createRadioStreamSession,
  type FileAudioSession,
  type RadioStreamSession,
} from "./radio_stream.js";
import { createTrackStream, type TrackStream } from "./track_stream.js";
import { ensureConnectionReady, watchConnectionLoss } from "./connection_ready.js";
import { toMessageReply } from "../runtime/discordjs_runtime.js";
import type { DiscordReply } from "../types.js";

// How long Ningo waits, alone in a voice channel, before leaving it.
export const ALONE_GRACE_MS = 3 * 60_000;

const require = createRequire(import.meta.url);
const prism = require("prism-media") as typeof import("prism-media");

type VoiceSession = {
  channelId: string;
  connection: ReturnType<typeof joinVoiceChannel>;
  player: ReturnType<typeof createAudioPlayer>;
  stream: RadioStreamSession | FileAudioSession;
  listening?: ListeningSession;
  // the music player has been tied to this connection
  musicHooked?: boolean;
};

// A server's music: its own player beside the one for speech and radio, so speech can pause the
// music and give the connection back to it afterwards.
type MusicSession = {
  player: ReturnType<typeof createAudioPlayer>;
  stream?: TrackStream;
  local: LocalPlayback;
  // the entry the player is playing; an Idle with one set is the track ending (or failing)
  playing?: number;
  // the queue's pause, apart from the pause while Ningo speaks
  userPaused: boolean;
  speaking: boolean;
  // paused because nobody is left in the voice channel, apart from the other two pauses
  away: boolean;
  // the next track's stream, started while this one plays so it starts at once
  prepared?: { entry: number; stream: TrackStream };
  volume: number;
  onEnded: (entry: number) => void;
  onGone: () => void;
};

export type MusicHooks = {
  // the track playing ended or could not start: the queue moves on
  onEnded: (entry: number) => void;
  // the voice connection is gone (Ningo was moved out or the channel closed): the queue stops
  onGone: () => void;
};

type ListeningSession = {
  targetUserId: string;
  stop(): void;
};

export type DiscordVoiceController = {
  attachClient(client?: unknown, botUserId?: string): void;
  playRadio(params: { guildId?: string; userId: string; streamUrl: string }): Promise<string>;
  speakText(params: { guildId?: string; userId: string; text: string }): Promise<string>;
  startConversation(params: {
    guildId?: string;
    userId: string;
    activationMode?: "always" | "wakeword";
    onTurn: (transcript: string) => Promise<void>;
  }): Promise<string>;
  stopConversation(params: { guildId?: string }): Promise<string>;
  stopRadio(params: { guildId?: string }): Promise<string>;
  // The voice channel the person is in, and the one Ningo plays music in, if any.
  memberVoiceChannel(params: { guildId: string; userId: string }): Promise<string | undefined>;
  musicChannel(params: { guildId: string }): string | undefined;
  // Posts or edits a message in a text channel (the music panel); the message id, when posted.
  postMessage(params: { channelId: string; message: DiscordReply }): Promise<string | undefined>;
  editMessage(params: { channelId: string; messageId: string; message: DiscordReply }): Promise<void>;
  // Brings the server's playback to the queue's state, joining the person's voice channel to play.
  applyMusic(params: { guildId: string; userId: string; state: QueueState; hooks: MusicHooks }): Promise<void>;
};

export function createDiscordVoiceController(options: {
  token: string;
  voiceServiceEndpoint: string;
  logTranscripts?: boolean;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
  ffmpegPath?: string;
  readyTimeoutMs?: number;
}): DiscordVoiceController {
  const sessions = new Map<string, VoiceSession>();
  const musics = new Map<string, MusicSession>();
  // A restart of the adapter must not leave yt-dlp or ffmpeg running.
  process.once("exit", () => {
    for (const music of musics.values()) {
      music.stream?.stop();
      music.prepared?.stream.stop();
    }
  });
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

    // Someone joined or left a voice channel: Ningo pauses when nobody is left with it, and
    // leaves when nobody comes back in time.
    client.on("voiceStateUpdate", (oldState, newState) => {
      for (const guildId of new Set([oldState.guild.id, newState.guild.id])) watchCompany(client, guildId);
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

  const aloneTimers = new Map<string, ReturnType<typeof setTimeout>>();
  // Whether anyone but bots is in Ningo's voice channel of a server; paused music waits for them.
  const watchCompany = (client: DiscordJsClient, guildId: string) => {
    const session = sessions.get(guildId);
    if (!session) return;
    const channel = client.channels.cache.get(session.channelId);
    const members = channel && "members" in channel ? channel.members : undefined;
    const company = members && typeof (members as { filter?: unknown }).filter === "function"
      ? (members as unknown as { filter(fn: (member: GuildMember) => boolean): { size: number } }).filter((member) => !member.user.bot).size
      : 1;
    const music = musics.get(guildId);
    if (company > 0) {
      clearTimeout(aloneTimers.get(guildId));
      aloneTimers.delete(guildId);
      if (music?.away) {
        music.away = false;
        if (!music.userPaused && !music.speaking) music.player.unpause();
      }
      return;
    }
    if (aloneTimers.has(guildId)) return;
    if (music && music.playing !== undefined) {
      music.away = true;
      music.player.pause();
    }
    options.log?.("[dokja-discord] alone in voice", JSON.stringify({ guildId, leaveInMs: ALONE_GRACE_MS }));
    aloneTimers.set(guildId, setTimeout(() => {
      aloneTimers.delete(guildId);
      const current = sessions.get(guildId);
      if (!current) return;
      // Leaving destroys the connection; a queue playing there is then reported stopped.
      stopGuildSession(current);
      sessions.delete(guildId);
      options.log?.("[dokja-discord] left an empty voice channel", JSON.stringify({ guildId }));
    }, ALONE_GRACE_MS));
  };

  // The bitrate Discord should encode music at: the voice channel's own, within 32 to 128 kbps.
  const channelBitrate = (guildId: string) => {
    const channelId = sessions.get(guildId)?.channelId;
    const channel = channelId ? voiceClient?.channels.cache.get(channelId) : undefined;
    const bitrate = channel && "bitrate" in channel && typeof channel.bitrate === "number" ? channel.bitrate : 64_000;
    return Math.min(128_000, Math.max(32_000, bitrate));
  };

  // Keeps the next track's stream ready, and lets go of one that is no longer next.
  const prepareNext = (music: MusicSession, next: QueueState["next"]) => {
    if (music.prepared && music.prepared.entry === next?.entry) return;
    dropPrepared(music);
    if (!next) return;
    try {
      music.prepared = { entry: next.entry, stream: createTrackStream(next.url, { ffmpegPath, log: options.log, error: options.error }) };
    } catch (error) {
      options.error?.("[dokja-discord] music prepare failed", error);
    }
  };

  // The server's music player, in the person's voice channel. A radio playing there is stopped:
  // the radio has no precedence.
  const musicSession = async ({ guildId, userId, hooks }: { guildId: string; userId: string; hooks: MusicHooks }) => {
    // Already in a voice channel of this server: the music stays there, whoever asked (the next
    // track plays even when the person who queued the last one has left).
    const existing = sessions.get(guildId);
    let session: VoiceSession;
    if (existing && existing.connection.state.status !== VoiceConnectionStatus.Destroyed) {
      try {
        await ensureConnectionReady(existing.connection, existing.channelId, readyTimeoutMs);
      } catch (error) {
        sessions.delete(guildId);
        stopGuildSession(existing);
        throw error;
      }
      session = existing;
    } else {
      const client = await ensureVoiceClient();
      const member = await fetchVoiceMember(client, guildId, userId);
      const voiceChannel = member.voice.channel;
      if (!voiceChannel) {
        throw new Error("Join a voice channel to play music.");
      }
      if (voiceChannel.type !== ChannelType.GuildVoice && voiceChannel.type !== ChannelType.GuildStageVoice) {
        throw new Error("Music plays only in voice channels.");
      }
      session = await ensureVoiceSession({ sessions, guildId, voiceChannel, readyTimeoutMs, log: options.log, error: options.error });
      sessions.set(guildId, session);
    }
    if (session.stream !== undefined && (session.stream as RadioStreamSession).sourceUrl) {
      interruptSessionPlayback(session);
    }
    let music = musics.get(guildId);
    if (!music || !session.musicHooked) {
      const player = createAudioPlayer({ behaviors: { noSubscriber: NoSubscriberBehavior.Pause } });
      const created: MusicSession = { player, local: IDLE, userPaused: false, speaking: false, away: false, volume: 100, onEnded: hooks.onEnded, onGone: hooks.onGone };
      player.on("error", (error) => options.error?.("[dokja-discord] music player error", error));
      // A track that ends, or fails to start, leaves the player idle with its entry still set.
      player.on(AudioPlayerStatus.Idle, () => {
        const entry = created.playing;
        if (entry === undefined) return;
        created.playing = undefined;
        created.stream?.stop();
        created.stream = undefined;
        created.onEnded(entry);
      });
      session.connection.on("stateChange", (_old, next) => {
        if (next.status !== VoiceConnectionStatus.Destroyed || musics.get(guildId) !== created) return;
        const wasPlaying = created.local.entry !== undefined;
        stopTrack(created);
        dropPrepared(created);
        musics.delete(guildId);
        if (wasPlaying) created.onGone();
      });
      session.musicHooked = true;
      music = created;
      musics.set(guildId, music);
    }
    session.connection.subscribe(music.player);
    return music;
  };

  return {
    attachClient(_client?: unknown) {
      void ensureVoiceClient();
    },
    async playRadio({ guildId, userId, streamUrl }) {
      if (!guildId) {
        throw new Error("Radio playback only works in guild voice channels");
      }
      // The radio has no precedence over a queue (owner, 2026-10-26).
      if (musics.get(guildId)?.local.entry !== undefined) {
        throw new Error("Music is playing in this server. Stop the queue with /stop before starting the radio.");
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

      const session = await ensureVoiceSession({
        sessions,
        guildId,
        voiceChannel,
        readyTimeoutMs,
        log: options.log,
        error: options.error,
      });
      const stream = createRadioStreamSession(streamUrl, {
        ffmpegPath,
        log: options.log,
        error: options.error,
      });

      replaceSessionStream(session, stream);
      session.player.play(stream.resource);

      try {
        await entersState(session.player, AudioPlayerStatus.Playing, 15_000);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        options.error?.(
          "[dokja-discord] radio player failed to start",
          JSON.stringify({
            guildId,
            voiceChannelId: voiceChannel.id,
            streamUrl,
            connectionStatus: session.connection.state.status,
            playerStatus: session.player.state.status,
            error: message,
          }),
        );
        stream.stop();
        if (session.connection.state.status === VoiceConnectionStatus.Destroyed) {
          sessions.delete(guildId);
        }
        throw new Error("Joined the voice channel, but the radio stream did not start playing.");
      }

      sessions.set(guildId, session);
      options.log?.(
        "[dokja-discord] radio started",
        JSON.stringify({ guildId, voiceChannelId: voiceChannel.id, streamUrl }),
      );
      return `Playing radio in <#${voiceChannel.id}>`;
    },
    async speakText({ guildId, userId, text }) {
      if (!guildId) {
        throw new Error("Speech playback only works in guild voice channels");
      }
      const trimmed = text.trim();
      if (!trimmed) {
        throw new Error("Speech text cannot be empty");
      }

      const client = await ensureVoiceClient();
      const member = await fetchVoiceMember(client, guildId, userId);
      const voiceChannel = member.voice.channel;

      if (!voiceChannel) {
        throw new Error("Join a voice channel before asking Ningo to speak");
      }
      if (
        voiceChannel.type !== ChannelType.GuildVoice &&
        voiceChannel.type !== ChannelType.GuildStageVoice
      ) {
        throw new Error("Speech playback only works in voice channels");
      }

      const response = await fetch(`${options.voiceServiceEndpoint}/tts`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          text: trimmed,
          voice: "default",
          language: "pt-BR",
          format: "wav",
        }),
      });
      if (!response.ok) {
        const detail = await response.text();
        throw new Error(`Voice synthesis failed: ${detail || response.status}`);
      }

      const wavBytes = Buffer.from(await response.arrayBuffer());
      const tempDir = mkdtempSync(join(tmpdir(), "dokja-voice-"));
      const tempPath = join(tempDir, "speech.wav");
      writeFileSync(tempPath, wavBytes);

      const session = await ensureVoiceSession({
        sessions,
        guildId,
        voiceChannel,
        readyTimeoutMs,
        log: options.log,
        error: options.error,
      });
      const stream = createFileAudioSession(tempPath, {
        ffmpegPath,
        log: options.log,
        error: options.error,
      });

      replaceSessionStream(session, stream);
      // Speech pauses the music and gives the connection back to it when it ends (owner, 2026-10-26).
      const music = musics.get(guildId);
      if (music?.playing !== undefined) {
        music.speaking = true;
        music.player.pause();
        session.connection.subscribe(session.player);
        session.player.once(AudioPlayerStatus.Idle, () => {
          music.speaking = false;
          if (musics.get(guildId) !== music || music.playing === undefined) return;
          session.connection.subscribe(music.player);
          if (!music.userPaused && !music.away) music.player.unpause();
        });
      }
      session.player.play(stream.resource);

      try {
        await entersState(session.player, AudioPlayerStatus.Playing, 15_000);
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        options.error?.(
          "[dokja-discord] speech player failed to start",
          JSON.stringify({
            guildId,
            voiceChannelId: voiceChannel.id,
            connectionStatus: session.connection.state.status,
            playerStatus: session.player.state.status,
            error: message,
          }),
        );
        stream.stop();
        if (session.connection.state.status === VoiceConnectionStatus.Destroyed) {
          sessions.delete(guildId);
        }
        throw new Error("Joined the voice channel, but speech playback did not start.");
      }

      sessions.set(guildId, session);
      options.log?.(
        "[dokja-discord] speech started",
        JSON.stringify({ guildId, voiceChannelId: voiceChannel.id, textChars: trimmed.length }),
      );
      return `Speaking in <#${voiceChannel.id}>`;
    },
    async postMessage({ channelId, message }) {
      const client = await ensureVoiceClient();
      const channel = await client.channels.fetch(channelId);
      if (!channel || !("send" in channel) || typeof channel.send !== "function") return undefined;
      // Song titles come from outside: the panel pings nobody, not even the person it names.
      const sent = await channel.send({ ...toMessageReply(message), allowedMentions: { parse: [] } } as never);
      return sent.id;
    },
    async editMessage({ channelId, messageId, message }) {
      const client = await ensureVoiceClient();
      const channel = await client.channels.fetch(channelId);
      if (!channel || !("messages" in channel)) return;
      const found = await channel.messages.fetch(messageId);
      await found.edit({ ...toMessageReply(message), allowedMentions: { parse: [] } } as never);
    },
    async memberVoiceChannel({ guildId, userId }) {
      const client = await ensureVoiceClient();
      const member = await fetchVoiceMember(client, guildId, userId);
      return member.voice.channel?.id;
    },
    musicChannel({ guildId }) {
      if (musics.get(guildId)?.local.entry === undefined) return undefined;
      return sessions.get(guildId)?.channelId;
    },
    async applyMusic({ guildId, userId, state, hooks }) {
      const current = musics.get(guildId);
      const step = reconcile(current?.local ?? IDLE, state);
      if (!step) return;
      for (const action of step.actions) {
        if (action.kind === "play") {
          const music = await musicSession({ guildId, userId, hooks });
          stopTrack(music);
          // The track made ready while the last one played starts at once; any other starts now.
          const ready = music.prepared?.entry === action.entry ? music.prepared.stream : undefined;
          if (!ready) dropPrepared(music);
          music.prepared = undefined;
          const stream = ready ?? createTrackStream(action.url, { ffmpegPath, log: options.log, error: options.error });
          stream.resource.volume?.setVolume(music.volume / 100);
          stream.resource.encoder?.setBitrate(channelBitrate(guildId));
          music.stream = stream;
          music.playing = action.entry;
          music.userPaused = false;
          music.player.play(stream.resource);
          options.log?.("[dokja-discord] music track", JSON.stringify({ guildId, entry: action.entry }));
          continue;
        }
        const music = musics.get(guildId);
        if (!music) continue;
        if (action.kind === "stop") {
          stopTrack(music);
          dropPrepared(music);
          sessions.get(guildId)?.connection.subscribe(sessions.get(guildId)!.player);
        } else if (action.kind === "pause") {
          music.userPaused = true;
          music.player.pause();
        } else if (action.kind === "resume") {
          music.userPaused = false;
          if (!music.speaking && !music.away) music.player.unpause();
        } else if (action.kind === "volume") {
          music.volume = action.volume;
          music.stream?.resource.volume?.setVolume(action.volume / 100);
        }
      }
      const music = musics.get(guildId);
      if (music) {
        // Only while something plays is there a next track worth making ready.
        prepareNext(music, step.next.entry === undefined ? undefined : state.next);
        music.local = step.next;
        music.onEnded = hooks.onEnded;
        music.onGone = hooks.onGone;
      }
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
    async startConversation({ guildId, userId, activationMode = "always", onTurn }) {
      if (!guildId) {
        throw new Error("Voice conversation only works in guild voice channels");
      }

      const client = await ensureVoiceClient();
      const member = await fetchVoiceMember(client, guildId, userId);
      const voiceChannel = member.voice.channel;

      if (!voiceChannel) {
        throw new Error("Join a voice channel before starting voice conversation");
      }
      if (
        voiceChannel.type !== ChannelType.GuildVoice &&
        voiceChannel.type !== ChannelType.GuildStageVoice
      ) {
        throw new Error("Voice conversation only works in voice channels");
      }

      const session = await ensureVoiceSession({
        sessions,
        guildId,
        voiceChannel,
        readyTimeoutMs,
        log: options.log,
        error: options.error,
      });

      session.listening?.stop();
      session.listening = createListeningSession({
        session,
        guildId,
        userId,
        voiceServiceEndpoint: options.voiceServiceEndpoint,
        logTranscripts: Boolean(options.logTranscripts),
        activationMode,
        log: options.log,
        error: options.error,
        onTurn,
      });
      sessions.set(guildId, session);

      options.log?.(
        "[dokja-discord] voice conversation started",
        JSON.stringify({ guildId, voiceChannelId: voiceChannel.id, userId, activationMode }),
      );
      if (activationMode === "wakeword") {
        return `Listening for the wake word in <#${voiceChannel.id}>`;
      }
      return `Listening to your voice in <#${voiceChannel.id}>`;
    },
    async stopConversation({ guildId }) {
      if (!guildId) {
        throw new Error("Voice conversation stop only works in guild voice channels");
      }
      const session = sessions.get(guildId);
      if (!session?.listening) {
        return "No voice conversation is active in this server.";
      }
      session.listening.stop();
      session.listening = undefined;
      options.log?.("[dokja-discord] voice conversation stopped", JSON.stringify({ guildId }));
      return "Voice conversation stopped.";
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
  session.listening?.stop();
  session.player.stop(true);
  session.stream.stop();
  session.connection.destroy();
}

function replaceSessionStream(
  session: VoiceSession,
  stream: RadioStreamSession | FileAudioSession,
) {
  interruptSessionPlayback(session);
  session.stream = stream;
}

function createListeningSession(params: {
  session: VoiceSession;
  guildId: string;
  userId: string;
  voiceServiceEndpoint: string;
  logTranscripts: boolean;
  activationMode: "always" | "wakeword";
  onTurn: (transcript: string) => Promise<void>;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
}): ListeningSession {
  const activeStreams = new Map<
    string,
    {
      destroy(): void;
    }
  >();

  const handleSpeakingStart = (speakingUserId: string) => {
    if (speakingUserId !== params.userId || activeStreams.has(speakingUserId)) {
      return;
    }

    if (params.session.player.state.status !== AudioPlayerStatus.Idle) {
      params.log?.(
        "[dokja-discord] interrupting voice playback for user speech",
        JSON.stringify({
          guildId: params.guildId,
          userId: speakingUserId,
          playerStatus: params.session.player.state.status,
        }),
      );
    }

    params.log?.(
      "[dokja-discord] voice turn capture started",
      JSON.stringify({ guildId: params.guildId, userId: speakingUserId }),
    );

    interruptSessionPlayback(params.session);

    const opusStream = params.session.connection.receiver.subscribe(speakingUserId, {
      end: {
        behavior: EndBehaviorType.AfterSilence,
        duration: 1_200,
      },
    });
    const decoder = new prism.opus.Decoder({
      frameSize: 960,
      channels: 2,
      rate: 48_000,
    });
    const pcmChunks: Buffer[] = [];
    let finished = false;

    const cleanup = () => {
      activeStreams.delete(speakingUserId);
      try {
        opusStream.destroy();
      } catch {
        // Ignore cleanup errors from already-closed Discord voice streams.
      }
      try {
        decoder.destroy();
      } catch {
        // Ignore cleanup errors from already-closed decoders.
      }
    };

    const finalize = async () => {
      if (finished) {
        return;
      }
      finished = true;

      const pcmBytes = Buffer.concat(pcmChunks);
      cleanup();
      if (pcmBytes.length < 48_000) {
        params.log?.(
          "[dokja-discord] voice turn ignored",
          JSON.stringify({ guildId: params.guildId, userId: speakingUserId, pcmBytes: pcmBytes.length }),
        );
        return;
      }

      try {
        const wavBytes = buildWavFromPCM(pcmBytes);
        let wakewordDetected = false;
        let wakewordScore: number | undefined;
        if (params.activationMode === "wakeword") {
          const wakeword = await detectWakeword({
            voiceServiceEndpoint: params.voiceServiceEndpoint,
            audioBytes: wavBytes,
            hotword: "Ningo",
          });
          wakewordDetected = wakeword.detected;
          wakewordScore = wakeword.score;
          params.log?.(
            "[dokja-discord] wakeword detection completed",
            JSON.stringify({
              guildId: params.guildId,
              userId: speakingUserId,
              detected: wakeword.detected,
              score: wakeword.score,
              hotword: wakeword.hotword,
            }),
          );
          if (!wakeword.detected) {
            params.log?.(
              "[dokja-discord] voice turn ignored missing wakeword",
              JSON.stringify({ guildId: params.guildId, userId: speakingUserId }),
            );
            return;
          }
        }

        const transcript = await transcribeVoiceTurn({
          voiceServiceEndpoint: params.voiceServiceEndpoint,
          audioBytes: wavBytes,
          language: "pt-BR",
        });
        if (!transcript) {
          return;
        }
        params.log?.(
          "[dokja-discord] voice transcription completed",
          JSON.stringify(
            params.logTranscripts
              ? {
                  guildId: params.guildId,
                  userId: speakingUserId,
                  textChars: transcript.length,
                  transcript,
                  activationMode: params.activationMode,
                  wakewordDetected,
                  wakewordScore,
                }
              : {
                  guildId: params.guildId,
                  userId: speakingUserId,
                  textChars: transcript.length,
                  activationMode: params.activationMode,
                  wakewordDetected,
                  wakewordScore,
                },
          ),
        );
        await params.onTurn(transcript);
      } catch (error) {
        params.error?.("[dokja-discord] voice turn processing failed", error);
      }
    };

    decoder.on("data", (chunk: Buffer) => {
      pcmChunks.push(Buffer.from(chunk));
    });
    decoder.once("end", () => {
      void finalize();
    });
    decoder.once("error", (error) => {
      params.error?.("[dokja-discord] voice decoder error", error);
      void finalize();
    });
    opusStream.once("error", (error) => {
      params.error?.("[dokja-discord] voice receive error", error);
      void finalize();
    });

    opusStream.pipe(decoder);
    activeStreams.set(speakingUserId, {
      destroy() {
        cleanup();
      },
    });
  };

  params.session.connection.receiver.speaking.on("start", handleSpeakingStart);

  return {
    targetUserId: params.userId,
    stop() {
      params.session.connection.receiver.speaking.off("start", handleSpeakingStart);
      for (const active of activeStreams.values()) {
        active.destroy();
      }
      activeStreams.clear();
    },
  };
}

async function ensureVoiceSession(params: {
  sessions: Map<string, VoiceSession>;
  guildId: string;
  voiceChannel: GuildBasedChannel;
  readyTimeoutMs: number;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
}): Promise<VoiceSession> {
  const existing = params.sessions.get(params.guildId);
  if (
    existing &&
    existing.channelId === params.voiceChannel.id &&
    existing.connection.state.status !== VoiceConnectionStatus.Destroyed
  ) {
    params.log?.(
      "[dokja-discord] reusing voice session",
      JSON.stringify({ guildId: params.guildId, voiceChannelId: params.voiceChannel.id }),
    );
    try {
      await ensureConnectionReady(existing.connection, existing.channelId, params.readyTimeoutMs);
    } catch (error) {
      params.sessions.delete(params.guildId);
      stopGuildSession(existing);
      throw error;
    }
    return existing;
  }

  stopGuildSession(existing);

  const connection = joinVoiceChannel({
    channelId: params.voiceChannel.id,
    guildId: params.guildId,
    adapterCreator: params.voiceChannel.guild.voiceAdapterCreator,
    selfDeaf: false,
  });

  connection.on("debug", (message) => {
    params.log?.(
      "[dokja-discord] discord.js voice debug",
      JSON.stringify({ guildId: params.guildId, channelId: params.voiceChannel.id, message }),
    );
  });
  connection.on("stateChange", (_oldState, newState) => {
    params.log?.(
      "[dokja-discord] voice connection state",
      JSON.stringify({
        guildId: params.guildId,
        voiceChannelId: params.voiceChannel.id,
        status: newState.status,
      }),
    );
  });

  try {
    await ensureConnectionReady(connection, params.voiceChannel.id, params.readyTimeoutMs);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    params.error?.(
      "[dokja-discord] voice ready wait failed",
      JSON.stringify({
        guildId: params.guildId,
        voiceChannelId: params.voiceChannel.id,
        status: connection.state.status,
        readyTimeoutMs: params.readyTimeoutMs,
        error: message,
      }),
    );
    connection.destroy();
    throw new Error(
      `Joined <#${params.voiceChannel.id}>, but the Discord voice transport never became ready. Check bot voice permissions and host networking and try again.`,
    );
  }

  const player = createAudioPlayer({
    behaviors: { noSubscriber: NoSubscriberBehavior.Play },
  });
  player.on("error", (error) => {
    params.error?.("[dokja-discord] voice player error", error);
  });
  player.on("stateChange", (_oldState, newState) => {
    params.log?.(
      "[dokja-discord] voice player state",
      JSON.stringify({ guildId: params.guildId, status: newState.status }),
    );
  });
  player.on(AudioPlayerStatus.Idle, () => {
    params.log?.("[dokja-discord] voice player idle", JSON.stringify({ guildId: params.guildId }));
  });

  connection.subscribe(player);

  const session: VoiceSession = {
    channelId: params.voiceChannel.id,
    connection,
    player,
    stream: createEmptyStream(),
  };
  watchConnectionLoss(connection, () => {
    if (params.sessions.get(params.guildId) !== session) return;
    params.log?.("[dokja-discord] voice connection lost", JSON.stringify({ status: connection.state.status }));
    params.sessions.delete(params.guildId);
    // Destroying the connection also invokes the music onGone hook: stop the
    // queue through the orchestrator and refresh its existing panel.
    stopGuildSession(session);
  });
  return session;
}

function createEmptyStream(): FileAudioSession {
  return {
    sourcePath: "",
    ffmpeg: null as never,
    resource: null as never,
    stop() {
      // No-op placeholder until a real stream is attached.
    },
  };
}

function interruptSessionPlayback(session: VoiceSession) {
  session.player.stop(true);
  session.stream.stop();
  session.stream = createEmptyStream();
}

async function transcribeVoiceTurn(params: {
  voiceServiceEndpoint: string;
  audioBytes: Buffer;
  language: string;
}): Promise<string> {
  const response = await fetch(`${params.voiceServiceEndpoint}/stt`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      audio_bytes_b64: params.audioBytes.toString("base64"),
      audio_format: "wav",
      language: params.language,
    }),
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(`Voice transcription failed: ${detail || response.status}`);
  }

  const payload = (await response.json()) as { text?: string };
  return (payload.text ?? "").trim();
}

async function detectWakeword(params: {
  voiceServiceEndpoint: string;
  audioBytes: Buffer;
  hotword: string;
}): Promise<{ detected: boolean; score: number; hotword: string }> {
  const response = await fetch(`${params.voiceServiceEndpoint}/wakeword`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      audio_bytes_b64: params.audioBytes.toString("base64"),
      audio_format: "wav",
      hotword: params.hotword,
    }),
  });
  if (!response.ok) {
    const detail = await response.text();
    throw new Error(`Wakeword detection failed: ${detail || response.status}`);
  }

  const payload = (await response.json()) as {
    detected?: boolean;
    score?: number;
    hotword?: string;
  };
  return {
    detected: Boolean(payload.detected),
    score: Number(payload.score ?? 0),
    hotword: String(payload.hotword ?? params.hotword),
  };
}

function buildWavFromPCM(pcmBytes: Buffer, sampleRate = 48_000, channels = 2, sampleWidth = 2): Buffer {
  const header = Buffer.alloc(44);
  const byteRate = sampleRate * channels * sampleWidth;
  const blockAlign = channels * sampleWidth;

  header.write("RIFF", 0);
  header.writeUInt32LE(36 + pcmBytes.length, 4);
  header.write("WAVE", 8);
  header.write("fmt ", 12);
  header.writeUInt32LE(16, 16);
  header.writeUInt16LE(1, 20);
  header.writeUInt16LE(channels, 22);
  header.writeUInt32LE(sampleRate, 24);
  header.writeUInt32LE(byteRate, 28);
  header.writeUInt16LE(blockAlign, 32);
  header.writeUInt16LE(sampleWidth * 8, 34);
  header.write("data", 36);
  header.writeUInt32LE(pcmBytes.length, 40);

  return Buffer.concat([header, pcmBytes]);
}

// Ends the music track without it counting as ended: no report, and both processes stop.
function stopTrack(music: MusicSession) {
  music.playing = undefined;
  music.player.stop(true);
  music.stream?.stop();
  music.stream = undefined;
}

// Lets go of a track made ready that will not play: both its processes end.
function dropPrepared(music: MusicSession) {
  music.prepared?.stream.stop();
  music.prepared = undefined;
}
