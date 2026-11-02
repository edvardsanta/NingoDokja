import { createWriteStream } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { Readable, Transform } from "node:stream";
import { pipeline } from "node:stream/promises";
import type { ReadableStream as WebReadableStream } from "node:stream/web";
import type { ReplyMedia } from "./bridge.js";

// A whole file over the slowest path the orchestrator allows itself.
const downloadTimeoutMs = 5 * 60 * 1000;

export type MediaSource = Buffer | {url: string};
export type WhatsAppMediaContent =
  | {image: MediaSource; caption: string; mimetype: string}
  | {video: MediaSource; caption: string; mimetype: string};

export function mediaContent(contentType: string, source: MediaSource, caption: string): WhatsAppMediaContent {
  return contentType === "video/mp4"
    ? {video: source, caption, mimetype: contentType}
    : {image: source, caption, mimetype: contentType};
}

// Never more than the size the orchestrator announced, whatever the server sends.
function bounded(size: number): Transform {
  let received = 0;
  return new Transform({
    transform(chunk: Buffer, _encoding, done) {
      received += chunk.length;
      done(received > size ? new Error("media is larger than announced") : null, chunk);
    },
    flush(done) {
      done(received === size ? null : new Error("media is shorter than announced"));
    },
  });
}

async function download(endpoint: string, media: ReplyMedia, file: string, fetchImpl: typeof fetch): Promise<void> {
  const signal = AbortSignal.timeout(downloadTimeoutMs);
  const response = await fetchImpl(`${endpoint.replace(/\/+$/, "")}/media/${media.media_token}`, {
    headers: {authorization: `Bearer ${process.env.DOKJA_GATEWAY_INGRESS_TOKEN ?? ""}`},
    signal,
  });
  if (!response.ok || !response.body) {
    throw new Error(`orchestrator media http ${response.status}`);
  }
  await pipeline(
    Readable.fromWeb(response.body as WebReadableStream),
    bounded(media.size ?? 0),
    createWriteStream(file, {flags: "wx", mode: 0o600}),
    {signal},
  );
}

// Streams a file named by token to disk, so Baileys can encrypt and upload it from there without
// holding it in memory, and removes it once `send` settles.
export async function withMediaFile<T>(
  endpoint: string,
  media: ReplyMedia,
  send: (file: string) => Promise<T>,
  fetchImpl: typeof fetch = fetch,
): Promise<T> {
  const folder = await mkdtemp(path.join(tmpdir(), "ningo-media-"));
  try {
    const file = path.join(folder, path.basename(media.filename));
    await download(endpoint, media, file, fetchImpl);
    return await send(file);
  } finally {
    await rm(folder, {recursive: true, force: true});
  }
}
