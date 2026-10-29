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
import type { Transform } from "node:stream";
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
import { LiveConversation, type VoiceTurnContext } from "./live_conversation.js";
import { voiceAudioRequest } from "./orchestrated_audio.js";
import { SpeechActivity } from "./speech_activity.js";
import { playSpeechQueue } from "./speech_queue.js";

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

type SpeakParams = {
  guildId?: string;
  userId: string;
  text: string;
  channelId?: string;
  originChannelId?: string;
  signal?: AbortSignal;
  waitForFloor?: () => Promise<void>;
};

export type DiscordVoiceController = {
  attachClient(client?: unknown, botUserId?: string): void;
  playRadio(params: { guildId?: string; userId: string; streamUrl: string }): Promise<string>;
  speakText(params: SpeakParams): Promise<string>;
  speakReply?(params: SpeakParams): Promise<void>;
  startConversation(params: {
    guildId?: string;
    userId: string;
    activationMode?: "always" | "wakeword";
    originChannelId?: string;
    onTurn: (transcript: string, turn?: VoiceTurnContext) => Promise<void>;
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
  orchestratorEndpoint?: string;
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

  async function renderSpeech({ guildId, userId, text, channelId, originChannelId, signal, waitForFloor,
    preparedAudio, waitForEnd,
  }: SpeakParams & { preparedAudio?: Buffer; waitForEnd?: boolean }): Promise<string> {
    if (!guildId) {
      throw new Error("Speech playback only works in guild voice channels");
    }
    const trimmed = text.trim();
    if (!trimmed) {
      throw new Error("Speech text cannot be empty");
    }

    signal?.throwIfAborted();
    const client = await ensureVoiceClient();
    const guild = client.guilds.cache.get(guildId) ?? await client.guilds.fetch(guildId);
    const voiceChannel = channelId
      ? guild.channels.cache.get(channelId) ?? await guild.channels.fetch(channelId)
      : (await fetchVoiceMember(client, guildId, userId)).voice.channel;

    if (!voiceChannel) {
      throw new Error("Join a voice channel before asking Ningo to speak");
    }
    if (
      voiceChannel.type !== ChannelType.GuildVoice &&
      voiceChannel.type !== ChannelType.GuildStageVoice
    ) {
      throw new Error("Speech playback only works in voice channels");
    }

    const wavBytes = preparedAudio ?? Buffer.from((await voiceAudioRequest(
      options.orchestratorEndpoint, "synthesize", {
        guildId, userId, channelId: originChannelId ?? voiceChannel.id, text: trimmed,
      }, signal,
    )).audio_bytes_b64 as string, "base64");
    signal?.throwIfAborted();
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

    if (signal?.aborted) { stream.stop(); signal.throwIfAborted(); }
    replaceSessionStream(session, stream);
    // Only explicit session cancellation stops playback. Human speech keeps
    // accumulating for the next turn while this reply finishes.
    const abortPlayback = () => {
      if (session.stream === stream) interruptSessionPlayback(session);
    };
    signal?.addEventListener("abort", abortPlayback, { once: true });
    session.player.once(AudioPlayerStatus.Idle, () => signal?.removeEventListener("abort", abortPlayback));
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
    await waitForFloor?.();
    signal?.throwIfAborted();
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
    if (waitForEnd) {
      const done = AbortSignal.any([
        AbortSignal.timeout(120_000), ...(signal ? [signal] : []),
      ]);
      await entersState(session.player, AudioPlayerStatus.Idle, done);
      stream.stop();
    }
    return `Speaking in <#${voiceChannel.id}>`;
  }

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
    speakText: renderSpeech,
    async speakReply(params) {
      if (!params.guildId) throw new Error("Speech playback only works in guild voice channels");
      await playSpeechQueue(params.text, {
        signal: params.signal,
        waitForFloor: params.waitForFloor,
        prepare: async (text) => {
          const audio = await voiceAudioRequest(options.orchestratorEndpoint, "synthesize", {
            guildId: params.guildId!, userId: params.userId,
            channelId: params.originChannelId ?? params.channelId!, text,
          }, params.signal);
          return Buffer.from(audio.audio_bytes_b64 as string, "base64");
        },
        play: async (text, preparedAudio) => {
          await renderSpeech({ ...params, text, preparedAudio, waitForEnd: true });
        },
      });
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
    async startConversation({ guildId, userId, originChannelId, activationMode = "always", onTurn }) {
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
        orchestratorEndpoint: options.orchestratorEndpoint,
        originChannelId: originChannelId ?? voiceChannel.id,
        participant: (id) => {
          const participant = voiceChannel.members.get(id);
          return participant && !participant.user.bot
            ? { userId: id, userName: participant.displayName } : undefined;
        },
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
      return `Listening to everyone in <#${voiceChannel.id}>`;
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

export const VOICE_SILENCE_MS = 600;
export const VOICE_SEGMENT_BYTES = 48_000 * 2 * 2 * 8; // eight seconds, stereo PCM16
export const MAX_VOICE_SPEAKERS = 16;

export function createListeningSession(params: {
  decoderFactory?: () => Transform;
  session: VoiceSession;
  guildId: string;
  userId: string;
  orchestratorEndpoint?: string;
  originChannelId: string;
  participant(id: string): { userId: string; userName?: string } | undefined;
  logTranscripts: boolean;
  activationMode: "always" | "wakeword";
  onTurn: (transcript: string, turn?: VoiceTurnContext) => Promise<void>;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
}): ListeningSession {
  let stopped = false;
  const lifetime = new AbortController();
  const activeStreams = new Map<string, { destroy(): void }>();
  const conversation = new LiveConversation({
    channelId: params.session.channelId,
    interrupt: () => interruptSessionPlayback(params.session),
    onTurn: params.onTurn,
    onError: (error) => params.error?.("[dokja-discord] voice reply failed", error),
  });
  // At most two transcriptions run at once; segment buffers and pending work are bounded.
  let transcribing = 0;
  const jobs: Array<() => Promise<void>> = [];
  const drain = () => {
    while (!stopped && transcribing < 2 && jobs.length) {
      transcribing++;
      void jobs.shift()!().finally(() => { transcribing--; drain(); });
    }
  };
  const handleSpeakingStart = (speakingUserId: string) => {
    const participant = params.participant(speakingUserId);
    if (stopped || !participant || activeStreams.has(speakingUserId)
      || activeStreams.size >= MAX_VOICE_SPEAKERS) return;
    let sequence = conversation.begin(false);
    const opusStream = params.session.connection.receiver.subscribe(speakingUserId, {
      end: { behavior: EndBehaviorType.AfterSilence, duration: VOICE_SILENCE_MS },
    });
    const decoder = params.decoderFactory?.() ?? new prism.opus.Decoder({ frameSize: 960, channels: 2, rate: 48_000 });
    let chunks: Buffer[] = [];
    let bytes = 0;
    let finished = false;
    let activity = new SpeechActivity();
    let hasSpeech = false;
    let lastSpeechByte = 0;
    let lastProbeByte = 0;
    let probing = false;
    const enqueue = (seq: number, audio: Buffer, speech: boolean) => {
      if (stopped || !speech || audio.length < 48_000) { conversation.finish(seq); return; }
      if (jobs.length >= 32) {
        conversation.finish(seq);
        params.error?.("[dokja-discord] voice transcription capacity reached");
        return;
      }
      conversation.transcribing(seq);
      jobs.push(async () => {
        try {
          const wav = buildWavFromPCM(audio).toString("base64");
          if (params.activationMode === "wakeword") {
            const wake = await voiceAudioRequest(params.orchestratorEndpoint, "wakeword", {
              guildId: params.guildId, ...participant, channelId: params.originChannelId,
              audio_bytes_b64: wav, hotword: "Ningo",
            }, lifetime.signal);
            if (!wake.detected) return;
          }
          const result = await voiceAudioRequest(params.orchestratorEndpoint, "transcribe", {
            guildId: params.guildId, ...participant, channelId: params.originChannelId,
            audio_bytes_b64: wav,
          }, lifetime.signal);
          // A speaker who left or moved rooms is never presented as still present.
          if (!stopped && params.participant(speakingUserId)) {
            conversation.finish(seq, { ...participant, text: String(result.text ?? "") });
          }
        } catch (error) {
          if (!lifetime.signal.aborted) params.error?.("[dokja-discord] voice transcription failed", error);
        } finally { conversation.finish(seq); }
      });
      drain();
    };
    const flushSegment = () => {
      const current = sequence;
      sequence = conversation.begin(false);
      enqueue(current, Buffer.concat(chunks, bytes), activity.transcribable);
      chunks = []; bytes = 0;
      activity = new SpeechActivity();
      hasSpeech = false;
      lastSpeechByte = 0;
      lastProbeByte = 0;
    };
    const probeSpeech = () => {
      const probeBytes = 61_440; // 320 ms of Discord stereo PCM
      if (probing || bytes - lastProbeByte < probeBytes || !activity.transcribable) return;
      const current = sequence;
      const probeEnd = bytes;
      const audio = Buffer.concat(chunks, bytes).subarray(Math.max(0, bytes - probeBytes));
      lastProbeByte = bytes;
      probing = true;
      void voiceAudioRequest(params.orchestratorEndpoint, "activity", {
        guildId: params.guildId, ...participant, channelId: params.originChannelId,
        audio_bytes_b64: buildWavFromPCM(audio).toString("base64"),
      }, lifetime.signal).then(result => {
        if (stopped || finished || current !== sequence) return;
        if (result.speech === true) {
          hasSpeech = true;
          lastSpeechByte = probeEnd;
          conversation.voiceActivity(current);
        } else if (hasSpeech && probeEnd - lastSpeechByte >= 122_880) {
          // End an utterance even if Discord keeps sending background-noise packets.
          flushSegment();
        }
      }).catch(error => {
        if (!lifetime.signal.aborted) params.error?.("[dokja-discord] voice activity probe failed", error);
      }).finally(() => { probing = false; });
    };
    const finalize = () => {
      if (finished) return;
      finished = true;
      activeStreams.delete(speakingUserId);
      enqueue(sequence, Buffer.concat(chunks, bytes), activity.transcribable);
      chunks = []; bytes = 0;
    };
    decoder.on("data", (chunk: Buffer) => {
      if (stopped) return;
      chunks.push(Buffer.from(chunk)); bytes += chunk.length;
      activity.push(chunk);
      probeSpeech();
      if (bytes >= VOICE_SEGMENT_BYTES) {
        flushSegment();
      }
    });
    decoder.once("end", finalize);
    decoder.once("error", () => { finalize(); opusStream.destroy(); });
    opusStream.once("error", () => { finalize(); decoder.destroy(); });
    activeStreams.set(speakingUserId, { destroy() {
      opusStream.destroy(); decoder.destroy(); chunks = []; bytes = 0;
    } });
    opusStream.pipe(decoder);
  };
  params.session.connection.receiver.speaking.on("start", handleSpeakingStart);
  return {
    targetUserId: params.userId,
    stop() {
      stopped = true;
      lifetime.abort();
      conversation.stop();
      jobs.length = 0;
      params.session.connection.receiver.speaking.off("start", handleSpeakingStart);
      for (const stream of activeStreams.values()) stream.destroy();
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
