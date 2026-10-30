import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import { Readable } from "node:stream";

// Chromium seeks by requesting byte ranges. Forwarding a file URL through net.fetch
// does not guarantee a partial response, so the app's scheme serves ranges itself.
export async function postMediaResponse(request: Request, file?: { path: string; type: string }): Promise<Response> {
  if (!file) return new Response(null, { status: 404 });
  if (request.method !== "GET" && request.method !== "HEAD") return new Response(null, { status: 405 });
  let size: number;
  try { size = (await stat(file.path)).size; }
  catch { return new Response(null, { status: 404 }); }
  const headers = new Headers({ "Content-Type": file.type, "Accept-Ranges": "bytes" });
  let start = 0;
  let end = size - 1;
  const range = request.method === "GET" ? request.headers.get("Range") : null;
  if (range) {
    const match = /^bytes=(\d*)-(\d*)$/.exec(range);
    let valid = Boolean(match && (match[1] || match[2]));
    if (match && valid) {
      if (match[1]) {
        start = Number(match[1]);
        end = match[2] ? Math.min(Number(match[2]), size - 1) : size - 1;
      } else {
        start = Math.max(0, size - Number(match[2]));
      }
      valid = Number.isSafeInteger(start) && Number.isSafeInteger(end) && start >= 0 && start <= end && start < size;
    }
    if (!valid) {
      headers.set("Content-Range", `bytes */${size}`);
      return new Response(null, { status: 416, headers });
    }
    headers.set("Content-Range", `bytes ${start}-${end}/${size}`);
  }
  headers.set("Content-Length", String(Math.max(0, end - start + 1)));
  const body = request.method === "HEAD" || size === 0 ? null :
    Readable.toWeb(createReadStream(file.path, { start, end })) as ReadableStream<Uint8Array>;
  return new Response(body, { status: range ? 206 : 200, headers });
}
