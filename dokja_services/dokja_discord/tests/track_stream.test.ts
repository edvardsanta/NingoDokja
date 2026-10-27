import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { createTrackStream } from "../voice/track_stream.js";

// Stand-ins: "yt-dlp" records its arguments and writes audio bytes slowly; "ffmpeg" passes them on.
function fakes() {
  const dir = mkdtempSync(join(tmpdir(), "track-stream-"));
  const args = join(dir, "args");
  const ytdlp = join(dir, "yt-dlp");
  writeFileSync(ytdlp, `#!/bin/sh\necho "$@" > ${args}\necho $$ > ${dir}/pid\nwhile true; do printf 'aaaa'; sleep 0.05; done\n`);
  const ffmpeg = join(dir, "ffmpeg");
  writeFileSync(ffmpeg, `#!/bin/sh\necho "$@" > ${dir}/ffmpeg-args\nexec cat\n`);
  chmodSync(ytdlp, 0o755);
  chmodSync(ffmpeg, 0o755);
  return { dir, args, ytdlp, ffmpeg };
}

const alive = (pid: number) => {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};

test("a track pipes yt-dlp into ffmpeg and stop ends both", async () => {
  const { dir, args, ytdlp, ffmpeg } = fakes();
  const stream = createTrackStream("https://play.example/watch?v=1", { ytdlpPath: ytdlp, ffmpegPath: ffmpeg });
  const chunk = await new Promise<Buffer>((resolve) => stream.resource.playStream.once("data", resolve));
  assert.ok(chunk.length > 0, "audio reaches the resource");
  const { readFileSync } = await import("node:fs");
  const used = readFileSync(args, "utf8");
  assert.match(used, /--js-runtimes node/);
  assert.match(used, /-o - -- https:\/\/play\.example\/watch\?v=1/);
  const decoded = readFileSync(join(dir, "ffmpeg-args"), "utf8");
  assert.match(decoded, /-af loudnorm=I=-16/, "every track is brought to the same loudness");
  assert.match(decoded, /-f s16le -ar 48000 -ac 2 pipe:1/);
  const pid = Number(readFileSync(join(dir, "pid"), "utf8"));
  assert.ok(alive(pid));
  stream.stop();
  await new Promise((resolve) => setTimeout(resolve, 200));
  assert.equal(alive(pid), false, "yt-dlp must not outlive the track");
});

test("only an http(s) page is played", () => {
  assert.throws(() => createTrackStream("file:///etc/passwd"));
  assert.throws(() => createTrackStream("-o/tmp/x"));
});
