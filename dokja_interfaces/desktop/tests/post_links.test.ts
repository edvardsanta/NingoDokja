import test from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { mkdtemp, readdir, readFile, utimes, mkdir, stat } from "node:fs/promises";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { ACTIONS, usePostStore } from "../src/main/actions.js";
import { isWriteAction, needsConfirmation } from "../src/shared/actions.js";
import { handleRequest } from "../src/main/ipc.js";
import { downloadMedia, MediaError } from "../src/main/media_fetch.js";
import { PostStore, type Download } from "../src/main/post_store.js";
import { parseConfig } from "../src/main/config.js";

const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
  "base64",
);
const MP4 = Buffer.concat([Buffer.from([0, 0, 0, 0x18]), Buffer.from("ftypmp42"), Buffer.alloc(40)]);
const DOC = "post-0123456789abcdef";

const digestReply = (result: Record<string, unknown>) => ({ event_id: "e", workflow: "digest", domain: "digest", result });

const okPost = (media: unknown[] = []) =>
  digestReply({
    status: "ok",
    post: {
      url: "https://example.com/p/1",
      document_id: DOC,
      author: "someone",
      caption: "First line\nsecond line",
      published: "2026-10-01T13:00:00Z",
      media,
      reader: "reader-a",
    },
  });

async function folders() {
  const root = await mkdtemp(join(tmpdir(), "post-links-test-"));
  return { temp: join(root, "temp"), saved: join(root, "saved"), root };
}

// A download that writes what it is told the address holds, or fails for an address it does not know.
function fakeDownload(files: Record<string, { type: string; bytes: Buffer }>): Download {
  return async (url, path) => {
    const file = files[url];
    if (!file) throw new MediaError("unavailable", "no");
    const { writeFile } = await import("node:fs/promises");
    await writeFile(path, file.bytes);
    return { kind: file.type.startsWith("video/") ? "video" : "image", type: file.type, bytes: file.bytes.length };
  };
}

test("digest.link.read needs a real press and sends only a web link", () => {
  assert.equal(isWriteAction("digest.link.read"), true);
  assert.equal(needsConfirmation("digest.link.read", { url: "https://example.com/p/1" }), false);
  const payload = ACTIONS["digest.link.read"].payload;
  assert.deepEqual(payload({ url: " https://example.com/p/1 ", extra: 1 }), { url: "https://example.com/p/1" });
  for (const url of ["", "file:///etc/passwd", "javascript:alert(1)", "https://user:pw@example.com/p"]) {
    assert.equal(payload({ url }), undefined, url);
  }
});

test("digest.link.read words an unknown refusal as failed and keeps a known one", () => {
  const project = ACTIONS["digest.link.read"].project;
  assert.deepEqual(project(digestReply({ status: "refused", refused: "login_required" })), { status: "refused", refused: "login_required" });
  assert.deepEqual(project(digestReply({ status: "refused", refused: "<b>raw</b>" })), { status: "refused", refused: "failed" });
  assert.deepEqual(project(digestReply({ status: "ok", post: { url: "https://example.com", document_id: "../x" } })), {
    status: "refused",
    refused: "failed",
  });
});

test("a held post shows its files only through the shell's scheme and counts what failed", async () => {
  const dirs = await folders();
  const store = new PostStore({
    savedRoot: dirs.saved,
    tempParent: dirs.root,
    download: fakeDownload({
      "https://media.example/v.mp4": { type: "video/mp4", bytes: MP4 },
      "https://media.example/i.png": { type: "image/png", bytes: PNG },
    }),
  });
  const projected = ACTIONS["digest.link.read"].project(
    okPost([
      { kind: "video", url: "https://media.example/v.mp4" },
      { kind: "image", url: "https://media.example/i.png" },
      { kind: "image", url: "https://media.example/gone.png" },
    ]),
  );
  const view = (await store.hold(projected)) as { status: string; post: { media: Array<{ kind: string; src: string }>; missing: number } };
  assert.equal(view.status, "ok");
  assert.deepEqual(view.post.media, [
    { kind: "video", src: `ningo-post://${DOC}/0` },
    { kind: "image", src: `ningo-post://${DOC}/1` },
  ]);
  assert.equal(view.post.missing, 1);
  assert.deepEqual(await readFile(store.file(`ningo-post://${DOC}/1`)!.path), PNG);
  assert.equal(store.file(`ningo-post://${DOC}/2`), undefined);
  assert.equal(store.file(`ningo-post://post-ffffffffffffffff/0`), undefined);
  assert.equal(store.file(`file://${DOC}/0`), undefined);

  await store.dispose();
  const left = await readdir(dirs.root);
  assert.deepEqual(left.filter((name) => name.startsWith("ningo-posts-")), [], "the temporary folder is erased");
});

test("saving a post copies its files and gives the Library its text, link and id", async () => {
  const dirs = await folders();
  const store = new PostStore({
    savedRoot: dirs.saved,
    tempParent: dirs.root,
    download: fakeDownload({ "https://media.example/i.png": { type: "image/png", bytes: PNG } }),
  });
  usePostStore(store);
  await store.hold(ACTIONS["digest.link.read"].project(okPost([{ kind: "image", url: "https://media.example/i.png" }])));

  const ingest = ACTIONS["knowledge.ingest"];
  assert.equal(ingest.payload({ mode: "post", post: "post-ffffffffffffffff" }), undefined, "only a post this run holds");
  const wire = ingest.payload({ mode: "post", post: DOC, body: "made up" })!;
  const prepared = await ingest.prepare!(wire);
  assert.deepEqual(await readdir(join(dirs.saved, DOC)), ["01.png"]);
  assert.equal(prepared!.source_id, DOC);
  assert.equal(prepared!.source_ref, "https://example.com/p/1");
  assert.equal(prepared!.kind, "post");
  assert.equal(prepared!.title, "First line");
  assert.match(String(prepared!.body), /^First line\nsecond line\n\nAuthor: someone\nPublished: 2026-10-01T13:00:00Z\nLink: https:\/\/example.com\/p\/1\nSaved files: 01.png$/);
  assert.deepEqual(await ingest.prepare!({ title: "t", body: "b" }), { title: "t", body: "b" }, "anything else goes as it was built");
  usePostStore(undefined);
  await store.dispose();
});

test("the shell saves first, then sends what it held, never what the page wrote", async () => {
  const dirs = await folders();
  const store = new PostStore({ savedRoot: dirs.saved, tempParent: dirs.root, download: fakeDownload({}) });
  usePostStore(store);
  await store.hold(ACTIONS["digest.link.read"].project(okPost()));
  const calls: Array<Record<string, unknown>> = [];
  const result = await handleRequest(
    { type: "knowledge.ingest", payload: { mode: "post", post: DOC, source_ref: "https://evil.example" } },
    {
      async request(_type, payload) {
        calls.push(payload);
        return { status: "ok", result: { workflow: "knowledge", domain: "knowledge", result: { created: true, changed: true } } };
      },
    },
    { defaultTimeoutMs: 1000, maxTimeoutMs: 1000 },
    undefined,
    { recent: () => true },
  );
  assert.equal(result.ok, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0]!.source_ref, "https://example.com/p/1");
  assert.equal("post" in calls[0]!, false);
  usePostStore(undefined);
  await store.dispose();
});

test("a stale temporary folder from a run that crashed is swept, a fresh one is kept", async () => {
  const dirs = await folders();
  const stale = join(dirs.root, "ningo-posts-old");
  const fresh = join(dirs.root, "ningo-posts-new");
  await mkdir(stale);
  await mkdir(fresh);
  const old = new Date(Date.now() - 2 * 24 * 60 * 60 * 1000);
  await utimes(stale, old, old);
  const store = new PostStore({ savedRoot: dirs.saved, tempParent: dirs.root, download: fakeDownload({}) });
  await store.sweep();
  await assert.rejects(stat(stale));
  assert.ok((await stat(fresh)).isDirectory());
});

async function serve(body: Buffer, type: string) {
  const server = http.createServer((_request, response) => {
    response.writeHead(200, { "content-type": type });
    response.end(body);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address() as AddressInfo;
  return { origin: `http://127.0.0.1:${port}`, port, close: () => new Promise((resolve) => server.close(resolve)) };
}

test("a post's file is streamed to disk, and only the named local origin may be on this machine", async () => {
  const dirs = await folders();
  await mkdir(dirs.temp, { recursive: true });
  const host = await serve(MP4, "video/mp4");
  try {
    await assert.rejects(downloadMedia(`${host.origin}/v.mp4`, join(dirs.temp, "a")), (error: MediaError) => error.code === "denied");
    const done = await downloadMedia(`${host.origin}/v.mp4`, join(dirs.temp, "b"), { localOrigin: host.origin });
    assert.deepEqual(done, { kind: "video", type: "video/mp4", bytes: MP4.length });
    assert.deepEqual(await readFile(join(dirs.temp, "b")), MP4);
    await assert.rejects(
      downloadMedia(`http://localhost:${host.port}/v.mp4`, join(dirs.temp, "c"), { localOrigin: host.origin }),
      (error: MediaError) => error.code === "denied",
      "another spelling of the origin is not the origin the owner named",
    );
  } finally {
    await host.close();
  }
});

test("a file that is not what its host said is removed", async () => {
  const dirs = await folders();
  await mkdir(dirs.temp, { recursive: true });
  const host = await serve(Buffer.from("<html>not a video</html>"), "video/mp4");
  try {
    await assert.rejects(
      downloadMedia(`${host.origin}/v.mp4`, join(dirs.temp, "v"), { localOrigin: host.origin }),
      (error: MediaError) => error.code === "unexpected",
    );
    await assert.rejects(stat(join(dirs.temp, "v")));
  } finally {
    await host.close();
  }
});

test("the posts folder and the readers' local origin are read from flags or the environment", () => {
  const config = parseConfig(["--posts-dir=/data/posts"], { DOKJA_POST_MEDIA_ORIGIN: "http://127.0.0.1:9000/x?y" }, "en");
  assert.equal(config.postsDir, "/data/posts");
  assert.equal(config.postMediaOrigin, "http://127.0.0.1:9000");
  assert.equal(parseConfig(["--post-media-origin=https://media.example"], {}, "en").postMediaOrigin, undefined);
});
