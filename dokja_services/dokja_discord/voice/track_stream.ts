import { spawn, type ChildProcess } from "node:child_process";

import { StreamType, createAudioResource, type AudioResource } from "@discordjs/voice";

// One track from a page: yt-dlp downloads its audio and pipes it to ffmpeg, which hands Discord
// raw 48 kHz stereo PCM. yt-dlp fetches the stream itself, so an address bound to the network that
// asked for it is never passed around. Both processes end with stop(), on skip, stop or shutdown.
export type TrackStream = {
  resource: AudioResource;
  stop(): void;
};

export type TrackStreamOptions = {
  ytdlpPath?: string;
  ffmpegPath?: string;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

export function createTrackStream(pageUrl: string, options: TrackStreamOptions = {}): TrackStream {
  if (!/^https?:\/\//.test(pageUrl)) throw new Error("a track plays only from an http(s) page");
  let stopped = false;
  const ytdlp = spawn(
    options.ytdlpPath ?? "yt-dlp",
    // node is already in the image: yt-dlp uses it to read pages that need JavaScript.
    ["--js-runtimes", "node", "-f", "bestaudio/best", "--no-playlist", "-q", "--no-warnings", "-o", "-", "--", pageUrl],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  const ffmpeg = spawn(
    options.ffmpegPath ?? "ffmpeg",
    // loudnorm brings every track to about the same loudness (-16 LUFS), so one song is not much
    // louder than the next.
    ["-loglevel", "error", "-i", "pipe:0", "-af", "loudnorm=I=-16:LRA=11:TP=-1.5", "-f", "s16le", "-ar", "48000", "-ac", "2", "pipe:1"],
    { stdio: ["pipe", "pipe", "pipe"] },
  );
  ytdlp.stdout?.pipe(ffmpeg.stdin!);
  // ffmpeg stops reading when it is stopped or the track ends: that is no error.
  ffmpeg.stdin?.on("error", () => {});
  ytdlp.stdout?.on("error", () => {});
  watch(ytdlp, "yt-dlp", () => stopped, options);
  watch(ffmpeg, "ffmpeg", () => stopped, options);

  const resource = createAudioResource(ffmpeg.stdout!, { inputType: StreamType.Raw, inlineVolume: true });
  return {
    resource,
    stop() {
      if (stopped) return;
      stopped = true;
      ytdlp.kill("SIGTERM");
      ffmpeg.kill("SIGTERM");
    },
  };
}

function watch(child: ChildProcess, name: string, stopped: () => boolean, options: TrackStreamOptions) {
  child.stderr?.on("data", (chunk) => {
    const line = String(chunk).trim();
    if (line && !stopped()) options.log?.(`[dokja-discord] music ${name}`, line.slice(0, 500));
  });
  child.on("error", (error) => {
    if (!stopped()) options.error?.(`[dokja-discord] music ${name} error`, error);
  });
}
