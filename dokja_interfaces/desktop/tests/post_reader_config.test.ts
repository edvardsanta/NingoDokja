import test from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { localPostMediaOrigin } from "../src/main/config.js";

test("the shell discovers only a valid local post media origin", (t) => {
  const root = mkdtempSync(join(tmpdir(), "post-reader-config-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  assert.equal(localPostMediaOrigin(root), undefined);
  assert.equal(localPostMediaOrigin(undefined), undefined);
  mkdirSync(join(root, ".dokja"));
  const file = join(root, ".dokja", "post-reader.json");
  for (const media_origin of ["https://example.com", "http://user:password@localhost:8791", "file:///tmp/video", null]) {
    writeFileSync(file, JSON.stringify({ media_origin }));
    assert.equal(localPostMediaOrigin(root), undefined);
  }
  writeFileSync(file, "invalid JSON");
  assert.equal(localPostMediaOrigin(root), undefined);
  writeFileSync(file, JSON.stringify({ media_origin: "http://127.0.0.1:8791/media" }));
  assert.equal(localPostMediaOrigin(root), "http://127.0.0.1:8791");
});
