import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { postMediaResponse } from "../src/main/post_media.js";

test("post media serves full files and seek ranges with matching headers", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "post-media-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const file = { path: join(root, "clip.mp4"), type: "video/mp4" };
  await writeFile(file.path, "0123456789");
  for (const [range, expected, contentRange] of [
    [undefined, "0123456789", null], ["bytes=4-7", "4567", "bytes 4-7/10"],
    ["bytes=6-", "6789", "bytes 6-9/10"], ["bytes=-3", "789", "bytes 7-9/10"],
    ["bytes=8-99", "89", "bytes 8-9/10"],
  ] as const) {
    const response = await postMediaResponse(new Request("ningo-post://post/0", { headers: range ? { Range: range } : {} }), file);
    assert.equal(response.status, range ? 206 : 200);
    assert.equal(response.headers.get("Accept-Ranges"), "bytes");
    assert.equal(response.headers.get("Content-Type"), "video/mp4");
    assert.equal(response.headers.get("Content-Range"), contentRange);
    assert.equal(response.headers.get("Content-Length"), String(expected.length));
    assert.equal(await response.text(), expected);
  }
  for (const range of ["bytes=10-", "bytes=7-3", "bytes=-0", "bytes=", "bytes=0-1,4-5", "invalid"]) {
    const response = await postMediaResponse(new Request("ningo-post://post/0", { headers: { Range: range } }), file);
    assert.equal(response.status, 416, range);
    assert.equal(response.headers.get("Content-Range"), "bytes */10");
  }
  const head = await postMediaResponse(new Request("ningo-post://post/0", { method: "HEAD" }), file);
  assert.equal(head.headers.get("Content-Length"), "10");
  assert.equal(await head.text(), "");
  assert.equal((await postMediaResponse(new Request("ningo-post://post/0"))).status, 404);
});
