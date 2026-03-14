import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";

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

const defaultFfmpegPath = "ffmpeg";

export function createRadioStreamSession(
  sourceUrl: string,
  options?: RadioStreamOptions,
): RadioStreamSession {
  const ffmpegPath = options?.ffmpegPath ?? defaultFfmpegPath;
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
    if (line !== "") {
      options?.log?.("[dokja-discord] radio ffmpeg", line);
    }
  });

  ffmpeg.on("error", (error) => {
    options?.error?.("[dokja-discord] radio ffmpeg error", error);
  });

  ffmpeg.on("exit", (code, signal) => {
    options?.log?.(
      "[dokja-discord] radio ffmpeg exit",
      JSON.stringify({ sourceUrl, code, signal }),
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
      ffmpeg.kill("SIGTERM");
    },
  };
}
