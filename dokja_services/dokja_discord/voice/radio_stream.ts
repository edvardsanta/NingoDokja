import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { rmSync } from "node:fs";

import { StreamType, createAudioResource, type AudioResource } from "@discordjs/voice";

type RadioStreamOptions = {
  ffmpegPath?: string;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

export type RadioStreamSession = {
  sourceUrl: string;
  ffmpeg: ChildProcessWithoutNullStreams;
  resource: AudioResource;
  stop(): void;
};

export type FileAudioSession = {
  sourcePath: string;
  ffmpeg: ChildProcessWithoutNullStreams;
  resource: AudioResource;
  stop(): void;
};

const defaultFfmpegPath = "ffmpeg";

export function createRadioStreamSession(
  sourceUrl: string,
  options?: RadioStreamOptions,
): RadioStreamSession {
  const ffmpegPath = options?.ffmpegPath ?? defaultFfmpegPath;
  let stopped = false;
  const ffmpeg = spawn(
    ffmpegPath,
    [
      "-reconnect",
      "1",
      "-reconnect_streamed",
      "1",
      "-reconnect_delay_max",
      "5",
      "-i",
      sourceUrl,
      "-analyzeduration",
      "0",
      "-loglevel",
      "warning",
      "-f",
      "s16le",
      "-ar",
      "48000",
      "-ac",
      "2",
      "pipe:1",
    ],
    {
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  ffmpeg.stderr.on("data", (chunk) => {
    const line = String(chunk).trim();
    if (!stopped && line !== "") {
      options?.log?.("[dokja-discord] radio ffmpeg", line);
    }
  });

  ffmpeg.on("error", (error) => {
    if (!stopped) {
      options?.error?.("[dokja-discord] radio ffmpeg error", error);
    }
  });

  ffmpeg.on("exit", (code, signal) => {
    options?.log?.(
      "[dokja-discord] radio ffmpeg exit",
      JSON.stringify({ sourceUrl, code, signal, stopped }),
    );
  });

  const resource = createAudioResource(ffmpeg.stdout, {
    inputType: StreamType.Raw,
    inlineVolume: false,
  });

  return {
    sourceUrl,
    ffmpeg,
    resource,
    stop() {
      stopped = true;
      ffmpeg.kill("SIGTERM");
    },
  };
}

export function createFileAudioSession(
  sourcePath: string,
  options?: RadioStreamOptions,
): FileAudioSession {
  const ffmpegPath = options?.ffmpegPath ?? defaultFfmpegPath;
  let stopped = false;
  const ffmpeg = spawn(
    ffmpegPath,
    [
      "-i",
      sourcePath,
      "-loglevel",
      "warning",
      "-f",
      "s16le",
      "-ar",
      "48000",
      "-ac",
      "2",
      "pipe:1",
    ],
    {
      stdio: ["ignore", "pipe", "pipe"],
    },
  );

  ffmpeg.stderr.on("data", (chunk) => {
    const line = String(chunk).trim();
    if (!stopped && line !== "") {
      options?.log?.("[dokja-discord] tts ffmpeg", line);
    }
  });

  ffmpeg.on("error", (error) => {
    if (!stopped) {
      options?.error?.("[dokja-discord] tts ffmpeg error", error);
    }
  });

  ffmpeg.on("exit", (code, signal) => {
    options?.log?.(
      "[dokja-discord] tts ffmpeg exit",
      JSON.stringify({ sourcePath, code, signal, stopped }),
    );
  });

  const resource = createAudioResource(ffmpeg.stdout, {
    inputType: StreamType.Raw,
    inlineVolume: false,
  });

  return {
    sourcePath,
    ffmpeg,
    resource,
    stop() {
      stopped = true;
      ffmpeg.kill("SIGTERM");
      try {
        rmSync(sourcePath, { force: true });
      } catch {
        // Best-effort cleanup for temporary TTS files.
      }
    },
  };
}
