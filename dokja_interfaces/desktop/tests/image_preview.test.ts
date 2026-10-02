import test from "node:test";
import assert from "node:assert/strict";

import { ImageError } from "../src/main/image_fetch.js";
import { ImageGate } from "../src/main/image_gate.js";
import { createPreviewer } from "../src/main/image_preview.js";
import { sleep } from "./support/reply_server.js";

const A = "https://images.example/a.png";
const B = "https://images.example/b.png";

function gateWith(...urls: string[]) {
  const gate = new ImageGate();
  gate.remember(urls);
  return gate;
}

test("an address the orchestrator did not list is refused without a fetch", async () => {
  const fetched: string[] = [];
  const preview = createPreviewer({
    gate: gateWith(A),
    fetchImage: async (url) => {
      fetched.push(url);
      return "data:image/png;base64,AA==";
    },
  });

  for (const url of [B, "", "file:///etc/passwd", 42, null, undefined, { url: A }]) {
    const result = await preview(url);
    assert.equal(result.ok, false);
    assert.ok(!result.ok && result.error.code === "denied", JSON.stringify(url));
  }
  assert.deepEqual(fetched, []);
});

test("a listed address is fetched and its data URL handed back", async () => {
  const preview = createPreviewer({ gate: gateWith(A), fetchImage: async () => "data:image/png;base64,AA==" });
  assert.deepEqual(await preview(A), { ok: true, dataUrl: "data:image/png;base64,AA==" });
});

test("a failed fetch is data with a code, and a surprise is unavailable", async () => {
  const slow = createPreviewer({
    gate: gateWith(A),
    fetchImage: async () => {
      throw new ImageError("timeout", "the image took too long");
    },
  });
  assert.deepEqual(await slow(A), { ok: false, error: { code: "timeout", message: "the image took too long" } });

  const broken = createPreviewer({
    gate: gateWith(A),
    fetchImage: async () => {
      throw new Error("something internal with details");
    },
  });
  assert.deepEqual(await broken(A), {
    ok: false,
    error: { code: "unavailable", message: "the image could not be fetched" },
  });
});

test("the same address asked twice at once is fetched once", async () => {
  let fetches = 0;
  const preview = createPreviewer({
    gate: gateWith(A),
    fetchImage: async () => {
      fetches += 1;
      await sleep(30);
      return "data:image/png;base64,AA==";
    },
  });

  const [first, second] = await Promise.all([preview(A), preview(A)]);
  assert.equal(fetches, 1);
  assert.deepEqual(first, second);

  await preview(A);
  assert.equal(fetches, 2, "once it is done the next ask fetches again");
});

test("only a few images are fetched at a time", async () => {
  const urls = Array.from({ length: 8 }, (_, index) => `https://images.example/${index}.png`);
  let running = 0;
  let peak = 0;
  const preview = createPreviewer({
    gate: gateWith(...urls),
    concurrency: 3,
    fetchImage: async () => {
      running += 1;
      peak = Math.max(peak, running);
      await sleep(20);
      running -= 1;
      return "data:image/png;base64,AA==";
    },
  });

  const results = await Promise.all(urls.map((url) => preview(url)));
  assert.equal(results.every((result) => result.ok), true);
  assert.equal(peak, 3);
});
