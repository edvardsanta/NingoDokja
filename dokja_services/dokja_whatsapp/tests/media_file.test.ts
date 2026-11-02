import assert from "node:assert/strict";
import { existsSync, readFileSync, statSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import test from "node:test";
import { Readable } from "node:stream";
import { mediaContent, withMediaFile } from "../transport/media_file.ts";
import { validatedReplyMedia } from "../messages/message_handler.ts";

const token = "a".repeat(48);
const media = {filename: "post-1.mp4", content_type: "video/mp4", media_token: token, size: 4};

async function serving(answer: (url: string, authorization: string | undefined) => {status: number; body: string}, use: (endpoint: string) => Promise<void>) {
  const server: Server = createServer((request, response) => {
    const {status, body} = answer(request.url ?? "", request.headers.authorization);
    response.statusCode = status;
    response.end(body);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    const endpoint = new URL("/orchestrator", "http://127.0.0.1");
    endpoint.port = String((server.address() as AddressInfo).port);
    await use(endpoint.toString());
  } finally {
    server.close();
  }
}

test("a file named by token is streamed with the gateway credential and gone after sending", async () => {
  process.env.DOKJA_GATEWAY_INGRESS_TOKEN = "fixture-credential";
  let seen = "";
  await serving((url, authorization) => {
    assert.equal(url, `/orchestrator/media/${token}`);
    assert.equal(authorization, "Bearer fixture-credential");
    return {status: 200, body: "clip"};
  }, async (endpoint) => {
    const result = await withMediaFile(endpoint, media, async (file) => {
      seen = file;
      assert.equal(readFileSync(file, "utf8"), "clip");
      return "sent";
    });
    assert.equal(result, "sent");
  });
  assert.equal(existsSync(seen), false);
});

test("the file is removed when sending fails", async () => {
  let seen = "";
  await serving(() => ({status: 200, body: "clip"}), async (endpoint) => {
    await assert.rejects(withMediaFile(endpoint, media, async (file) => {
      seen = file;
      throw new Error("upload failed");
    }), /upload failed/);
  });
  assert.equal(existsSync(seen), false);
});

test("nothing is sent when the file is not exactly what was announced", async () => {
  for (const answer of [{status: 200, body: "cli"}, {status: 200, body: "clips"}, {status: 404, body: ""}]) {
    await serving(() => answer, async (endpoint) => {
      await assert.rejects(withMediaFile(endpoint, media, async () => assert.fail("sent a bad file")));
    });
  }
});

test("a filename cannot leave the download folder", async () => {
  await serving(() => ({status: 200, body: "clip"}), async (endpoint) => {
    await withMediaFile(endpoint, {...media, filename: "../../escape.mp4"}, async (file) => {
      assert.match(file, /ningo-media-[^/]+\/escape\.mp4$/);
    });
  });
});

test("media content sends a path or bytes as the right kind", () => {
  assert.deepEqual(mediaContent("video/mp4", {url: "/tmp/clip"}, "caption"), {video: {url: "/tmp/clip"}, caption: "caption", mimetype: "video/mp4"});
  const bytes = Buffer.from("x");
  assert.deepEqual(mediaContent("image/png", bytes, ""), {image: bytes, caption: "", mimetype: "image/png"});
});

test("media by token is limited to 100 MB per file and in total", () => {
  const file = (size: number) => ({...media, size});
  assert.equal(validatedReplyMedia([file(92_923_063)]).length, 1);
  assert.equal(validatedReplyMedia([file(100_000_000)]).length, 1);
  assert.throws(() => validatedReplyMedia([file(100_000_001)]));
  assert.throws(() => validatedReplyMedia([file(60_000_000), file(60_000_000)]));
  for (const size of [0, -1, 1.5, undefined]) {
    assert.throws(() => validatedReplyMedia([{...media, size}]));
  }
  for (const bad of ["short", "A".repeat(48), `${token}/../x`]) {
    assert.throws(() => validatedReplyMedia([{...media, media_token: bad}]));
  }
  const inline = {filename: "x.png", content_type: "image/png", data_b64: Buffer.alloc(2).toString("base64")};
  assert.equal(validatedReplyMedia([file(10), inline]).length, 2);
  assert.throws(() => validatedReplyMedia([file(100_000_000), inline]));
});


test("a 100 MB file reaches the sender on disk through bounded chunks", async () => {
  const size = 100_000_000;
  let chunks = 0;
  const fetchImpl = (async () => new Response(Readable.toWeb(Readable.from((function* () {
    const chunk = Buffer.alloc(64 * 1024, 42);
    for (let offset = 0; offset < size; offset += chunk.length) {
      chunks++;
      yield chunk.subarray(0, Math.min(chunk.length, size - offset));
    }
  })())) as ReadableStream<Uint8Array>)) as typeof fetch;
  let seen = "";
  await withMediaFile("http://orchestrator", {...media, size}, async (file) => {
    seen = file;
    assert.equal(statSync(file).size, size);
    assert.ok(chunks > 1);
  }, fetchImpl);
  assert.equal(existsSync(seen), false);
});
