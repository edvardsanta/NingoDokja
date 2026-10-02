import { lookup as dnsLookup, type LookupAddress } from "node:dns";
import type { IncomingMessage } from "node:http";
import http from "node:http";
import https from "node:https";
import { BlockList, isIP, type LookupFunction } from "node:net";

import type { TransportErrorCode } from "../shared/transport.js";

const MAX_IMAGE_BYTES = 4 * 1024 * 1024;
// A meme video is a short clip, a few megabytes; the cap keeps one request from filling the shell's memory.
const MAX_VIDEO_BYTES = 20 * 1024 * 1024;
// How long a host may take to start answering, and how long the whole download may take: a video
// is slower to arrive than a picture, but a host that says nothing is given up on sooner.
const HEADER_TIMEOUT_MS = 12_000;
const TIMEOUT_MS = 30_000;
const MAX_REDIRECTS = 3;

// Many hosts refuse a client that does not look like a browser or that hot-links, as the TUI also
// works around: a generic browser identity and a Referer from the host's own site.
const USER_AGENT =
  "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0 Safari/537.36";

export class MediaError extends Error {
  readonly code: TransportErrorCode;

  constructor(code: TransportErrorCode, message: string) {
    super(message);
    this.name = "MediaError";
    this.code = code;
  }
}

// Options exist so tests can reach a server on this machine. The shell passes none.
export type MediaOptions = {
  guard?: (address: string) => boolean;
  ports?: number[];
  // Replaces the size cap of both kinds (the defaults are 4 MiB for a picture, 20 MiB for a video).
  maxBytes?: number;
  // Replaces the whole time limit; the wait for the first byte is the shorter of this and 12 s.
  timeoutMs?: number;
};

const NOT_PUBLIC = new BlockList();
for (const [network, prefix] of [
  ["0.0.0.0", 8],
  ["10.0.0.0", 8],
  ["100.64.0.0", 10],
  ["127.0.0.0", 8],
  ["169.254.0.0", 16],
  ["172.16.0.0", 12],
  ["192.0.0.0", 24],
  ["192.0.2.0", 24],
  ["192.88.99.0", 24],
  ["192.168.0.0", 16],
  ["198.18.0.0", 15],
  ["198.51.100.0", 24],
  ["203.0.113.0", 24],
  ["224.0.0.0", 4],
  ["240.0.0.0", 4],
] as const) {
  NOT_PUBLIC.addSubnet(network, prefix, "ipv4");
}
for (const [network, prefix] of [
  ["::", 128],
  ["::1", 128],
  ["64:ff9b::", 96],
  ["100::", 64],
  ["2001::", 23],
  ["2001:db8::", 32],
  ["2002::", 16],
  ["fc00::", 7],
  ["fe80::", 10],
  ["ff00::", 8],
] as const) {
  NOT_PUBLIC.addSubnet(network, prefix, "ipv6");
}

// Loopback, private, link-local, shared, documentation, multicast and reserved ranges are not
// public, and an IPv4 address wrapped in IPv6 counts as the IPv4 address.
export function isPublicAddress(address: string): boolean {
  const family = isIP(address);
  if (family === 0) return false;
  return !NOT_PUBLIC.check(address, family === 4 ? "ipv4" : "ipv6");
}

// The check runs on the very addresses the connection then uses, so a host cannot answer a public
// address to a check and a private one to the connection.
function guardedLookup(guard: (address: string) => boolean): LookupFunction {
  return (hostname, options, callback) => {
    dnsLookup(hostname, { all: true }, (error, addresses) => {
      if (error) return callback(error, "", 0);
      const [first] = addresses;
      if (!first || !addresses.every((entry) => guard(entry.address))) {
        return callback(Object.assign(new Error("address not allowed"), { code: "EBLOCKED" }), "", 0);
      }
      if (options.all) {
        // With "all" the callback takes the whole list; the declared type only has the single form.
        return (callback as unknown as (error: null, list: LookupAddress[]) => void)(null, addresses);
      }
      return callback(null, first.address, first.family);
    });
  };
}

function checkUrl(raw: string, ports: number[], guard: (address: string) => boolean): URL {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    throw new MediaError("invalid", "not an address");
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new MediaError("invalid", "only http and https addresses are previewed");
  }
  if (url.username !== "" || url.password !== "") {
    throw new MediaError("denied", "an address with credentials is not previewed");
  }
  const port = url.port !== "" ? Number(url.port) : url.protocol === "https:" ? 443 : 80;
  if (!ports.includes(port)) throw new MediaError("denied", "that port is not previewed");

  // Node asks the lookup only for names. An address written as numbers (127.0.0.1, [::1], and the
  // forms the URL parser turns into them, such as 2130706433) connects directly, so it is checked here.
  const host = url.hostname.startsWith("[") ? url.hostname.slice(1, -1) : url.hostname;
  if (isIP(host) !== 0 && !guard(host)) {
    throw new MediaError("denied", "that host is not on a public address");
  }
  return url;
}

function toMediaError(error: unknown, signal: AbortSignal): MediaError {
  if (error instanceof MediaError) return error;
  if ((error as NodeJS.ErrnoException)?.code === "ETIMEDOUT") {
    return new MediaError("timeout", "the host took too long to answer");
  }
  if (signal.aborted) return new MediaError("timeout", "the download took too long");
  if ((error as NodeJS.ErrnoException)?.code === "EBLOCKED") {
    return new MediaError("denied", "that host is not on a public address");
  }
  return new MediaError("unavailable", "the host did not answer");
}

function get(
  url: URL,
  lookup: LookupFunction,
  signal: AbortSignal,
  headerTimeoutMs: number,
): Promise<IncomingMessage> {
  const client = url.protocol === "https:" ? https : http;
  return new Promise((resolve, reject) => {
    const request = client.request(
      url,
      {
        method: "GET",
        agent: false,
        lookup,
        signal,
        // Host goes first, as every browser and curl send it. Node would add it last, and a host
        // behind a bot filter answers 403 to a request that does not start the way a browser's does.
        headers: {
          Host: url.host,
          "User-Agent": USER_AGENT,
          Accept:
            "video/mp4,video/webm;q=0.9,video/*;q=0.8,image/avif,image/webp,image/png,image/jpeg,image/gif,image/*;q=0.7",
          Referer: `${url.origin}/`,
        },
      },
      (response) => {
        clearTimeout(timer);
        resolve(response);
      },
    );
    // The total limit covers a slow download; this one gives up on a host that says nothing.
    const timer = setTimeout(
      () => request.destroy(Object.assign(new Error("no answer"), { code: "ETIMEDOUT" })),
      headerTimeoutMs,
    );
    request.on("error", (error) => {
      clearTimeout(timer);
      reject(toMediaError(error, signal));
    });
    request.end();
  });
}

// Only formats a browser shows as a picture. SVG is left out on purpose: it is a document.
export function sniffImage(bytes: Buffer): string | undefined {
  const starts = (...values: number[]) => values.every((value, index) => bytes[index] === value);
  const text = (from: number, value: string) => bytes.subarray(from, from + value.length).toString("latin1") === value;
  if (starts(0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a)) return "image/png";
  if (starts(0xff, 0xd8, 0xff)) return "image/jpeg";
  if (text(0, "GIF87a") || text(0, "GIF89a")) return "image/gif";
  if (text(0, "RIFF") && text(8, "WEBP")) return "image/webp";
  if (text(4, "ftypavif") || text(4, "ftypavis")) return "image/avif";
  return undefined;
}

// Only the containers a browser plays in a video element: MP4 (QuickTime too) and WebM. A file of
// the MP4 family that is really a picture (AVIF, HEIC) is not a video.
const PICTURE_BRANDS = new Set(["avif", "avis", "heic", "heix", "hevx", "mif1", "msf1"]);

export function sniffVideo(bytes: Buffer): string | undefined {
  if (bytes.subarray(4, 8).toString("latin1") === "ftyp") {
    return PICTURE_BRANDS.has(bytes.subarray(8, 12).toString("latin1")) ? undefined : "video/mp4";
  }
  if (bytes[0] === 0x1a && bytes[1] === 0x45 && bytes[2] === 0xdf && bytes[3] === 0xa3) return "video/webm";
  return undefined;
}

// What the host says the file is decides which cap and which check apply. The bytes must then agree
// with it, so a file cannot be passed off as the other kind.
type Kind = "image" | "video";

function kindOf(declared: string): Kind | undefined {
  if (declared.startsWith("image/") && !declared.startsWith("image/svg")) return "image";
  if (declared.startsWith("video/")) return "video";
  return undefined;
}

async function readMedia(
  response: IncomingMessage,
  maxBytes: number | undefined,
  signal: AbortSignal,
): Promise<string> {
  const kind = kindOf(String(response.headers["content-type"] ?? "").toLowerCase());
  if (!kind) {
    response.destroy();
    throw new MediaError("unexpected", "that is not a picture or a video");
  }
  const cap = maxBytes ?? (kind === "video" ? MAX_VIDEO_BYTES : MAX_IMAGE_BYTES);
  const length = Number(response.headers["content-length"] ?? 0);
  if (length > cap) {
    response.destroy();
    throw new MediaError("unexpected", "that file is too large");
  }

  const chunks: Buffer[] = [];
  let received = 0;
  try {
    for await (const chunk of response as AsyncIterable<Buffer>) {
      received += chunk.length;
      if (received > cap) {
        response.destroy();
        throw new MediaError("unexpected", "that file is too large");
      }
      chunks.push(chunk);
    }
  } catch (error) {
    throw toMediaError(error, signal);
  }

  const bytes = Buffer.concat(chunks);
  const type = kind === "video" ? sniffVideo(bytes) : sniffImage(bytes);
  if (!type) throw new MediaError("unexpected", "that is not a picture or a video");
  return `data:${type};base64,${bytes.toString("base64")}`;
}

// Fetches one picture or video for a preview and returns it as a data URL. It is strict: http or
// https on the default ports, no credentials, public addresses only (checked when connecting, and
// again after every redirect), at most three redirects, a size cap and a time limit, and the bytes
// must be what the host said they are: a picture, or an MP4 or WebM video. Nothing is sent but a
// plain GET: no cookies and no authentication.
export async function fetchMedia(rawUrl: string, options: MediaOptions = {}): Promise<string> {
  const ports = options.ports ?? [80, 443];
  const totalMs = options.timeoutMs ?? TIMEOUT_MS;
  const signal = AbortSignal.timeout(totalMs);
  const guard = options.guard ?? isPublicAddress;
  const lookup = guardedLookup(guard);

  let url = checkUrl(rawUrl, ports, guard);
  for (let redirects = 0; ; redirects += 1) {
    const response = await get(url, lookup, signal, Math.min(HEADER_TIMEOUT_MS, totalMs));
    const status = response.statusCode ?? 0;
    const next = response.headers.location;
    if (status >= 300 && status < 400 && next) {
      response.resume();
      if (redirects >= MAX_REDIRECTS) throw new MediaError("unexpected", "too many redirects");
      url = checkUrl(new URL(next, url).href, ports, guard);
      continue;
    }
    if (status !== 200) {
      response.resume();
      throw new MediaError("unavailable", `the host answered ${status}`);
    }
    return readMedia(response, options.maxBytes, signal);
  }
}
