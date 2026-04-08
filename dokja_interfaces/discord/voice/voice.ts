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

import {
  createFileAudioSession,
  createRadioStreamSession,
  type FileAudioSession,
  type RadioStreamSession,
} from "./radio_stream.js";

const require = createRequire(import.meta.url);
const prism = require("prism-media") as typeof import("prism-media");

type VoiceSession = {
  channelId: string;
  connection: ReturnType<typeof joinVoiceChannel>;
  player: ReturnType<typeof createAudioPlayer>;
  stream: RadioStreamSession | FileAudioSession;
  listening?: ListeningSession;
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
    await entersState(connection, VoiceConnectionStatus.Ready, params.readyTimeoutMs);
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

  return {
    channelId: params.voiceChannel.id,
    connection,
    player,
    stream: createEmptyStream(),
  };
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
