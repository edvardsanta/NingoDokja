import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

import { HistoryStore, pageSize } from "../history/store.js";

const day = 24 * 60 * 60 * 1000;

async function withStore(
  run: (store: HistoryStore, dir: string, clock: { now: number }) => Promise<void>,
  retentionDays = 30,
) {
  const dir = await mkdtemp(path.join(tmpdir(), "dokja-history-"));
  const clock = { now: Date.UTC(2026, 9, 11, 12) };
  try {
    const store = new HistoryStore({ dir: path.join(dir, "history"), retentionMs: retentionDays * day, now: () => clock.now });
    await store.init();
    await run(store, path.join(dir, "history"), clock);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

const message = (n: number, at: number, text = `message ${n}`) => ({ id: `id-${n}`, at, who: "contact" as const, text });

test("messages come back oldest first, with a growing sequence number", async () => {
  await withStore(async (store, _dir, clock) => {
    await store.append("friend", message(1, clock.now - 3000));
    await store.append("friend", { ...message(2, clock.now - 2000), who: "bot" });
    await store.append("friend", { ...message(3, clock.now - 1000), who: "owner" });

    const page = await store.page("friend");

    assert.deepEqual(page.messages.map((entry) => [entry.seq, entry.who, entry.text]), [
      [1, "contact", "message 1"],
      [2, "bot", "message 2"],
      [3, "owner", "message 3"],
    ]);
    assert.equal(page.before, "", "a page that is not full has no older page");
  });
});

test("a full page gives the cursor of the older one, and the older page ends where the newer began", async () => {
  await withStore(async (store, _dir, clock) => {
    for (let n = 1; n <= pageSize + 5; n += 1) {
      await store.append("friend", message(n, clock.now - 100_000 + n));
    }

    const latest = await store.page("friend");
    assert.equal(latest.messages.length, pageSize);
    assert.equal(latest.messages[0].seq, 6);
    assert.equal(latest.before, "6");

    const older = await store.page("friend", Number(latest.before));
    assert.deepEqual(older.messages.map((entry) => entry.seq), [1, 2, 3, 4, 5]);
    assert.equal(older.before, "");
  });
});

test("the same message id is recorded once, and an empty text not at all", async () => {
  await withStore(async (store, _dir, clock) => {
    await store.append("friend", message(1, clock.now));
    await store.append("friend", { ...message(1, clock.now), text: "edited copy" });
    await store.append("friend", message(2, clock.now, "   "));

    const page = await store.page("friend");

    assert.deepEqual(page.messages.map((entry) => entry.text), ["message 1"]);
  });
});

test("a long text is cut", async () => {
  await withStore(async (store, _dir, clock) => {
    await store.append("friend", message(1, clock.now, "x".repeat(10_000)));

    assert.equal((await store.page("friend")).messages[0].text.length, 4000);
  });
});

test("what is past the retention is forgotten, in memory and on disk", async () => {
  await withStore(async (store, dir, clock) => {
    await store.append("friend", message(1, clock.now - 31 * day, "old"));
    await store.append("friend", message(2, clock.now - 1 * day, "recent"));
    await store.append("gone", message(3, clock.now - 40 * day, "only old"));

    await store.purge();

    assert.deepEqual((await store.page("friend")).messages.map((entry) => entry.text), ["recent"]);
    assert.deepEqual((await store.page("gone")).messages, []);
    const file = await readFile(path.join(dir, "friend.jsonl"), "utf8");
    assert.ok(!file.includes("old"), "an expired message does not stay in the file");
    await assert.rejects(stat(path.join(dir, "gone.jsonl")), "an emptied conversation leaves no file");
  });
});

test("a new store reads what an earlier one wrote and skips a damaged line", async () => {
  await withStore(async (store, dir, clock) => {
    await store.append("friend", message(1, clock.now));
    await store.append("friend", message(2, clock.now));
    await writeFile(path.join(dir, "friend.jsonl"), `${await readFile(path.join(dir, "friend.jsonl"), "utf8")}{not json\n`);

    const again = new HistoryStore({ dir, retentionMs: 30 * day, now: () => clock.now });
    const page = await again.page("friend");

    assert.deepEqual(page.messages.map((entry) => entry.seq), [1, 2]);
    await again.append("friend", message(3, clock.now));
    assert.equal((await again.page("friend")).messages.at(-1)?.seq, 3);
  });
});

test("the folder and the files are readable by their owner only", async () => {
  await withStore(async (store, dir, clock) => {
    await store.append("friend", message(1, clock.now));

    assert.equal((await stat(dir)).mode & 0o777, 0o700);
    assert.equal((await stat(path.join(dir, "friend.jsonl"))).mode & 0o777, 0o600);
  });
});

test("an alias that could name another path is refused before any file is touched", async () => {
  await withStore(async (store, _dir, clock) => {
    for (const alias of ["../escape", "a/b", "", "UPPER", "x".repeat(40), ".hidden"]) {
      await assert.rejects(store.append(alias, message(1, clock.now)), /invalid conversation alias/, alias);
    }
  });
});

test("a conversation keeps its newest entries up to a limit", async () => {
  await withStore(async (store, _dir, clock) => {
    for (let n = 1; n <= 2005; n += 1) {
      await store.append("busy", message(n, clock.now));
    }

    const all = await store.page("busy", 10_000);
    assert.equal(all.messages.at(-1)?.seq, 2005);
    const oldest = await store.page("busy", 6);
    assert.deepEqual(oldest.messages, [], "the first entries were dropped");
  });
});
