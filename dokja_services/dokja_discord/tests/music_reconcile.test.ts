import assert from "node:assert/strict";
import test from "node:test";

import { IDLE, parseQueueState, reconcile, type QueueState } from "../music/reconcile.js";

const state = (overrides: Partial<QueueState> = {}): QueueState => ({
  epoch: "e1", version: 1, paused: false, volume: 100, waiting: 0,
  nowPlaying: { entry: 1, url: "https://play.example/1", title: "One", uploader: "Band", requestedBy: "", thumbnail: "" },
  ...overrides,
});

test("a first track plays, at its volume and paused state", () => {
  const result = reconcile(IDLE, state({ volume: 40, paused: true }));
  assert.deepEqual(result?.actions, [
    { kind: "play", entry: 1, url: "https://play.example/1" },
    { kind: "volume", volume: 40 },
    { kind: "pause" },
  ]);
  assert.deepEqual(result?.next, { epoch: "e1", version: 1, entry: 1, paused: true, volume: 40 });
});

test("the same track only changes what changed", () => {
  const local = reconcile(IDLE, state())!.next;
  assert.deepEqual(reconcile(local, state({ version: 2, paused: true }))?.actions, [{ kind: "pause" }]);
  assert.deepEqual(reconcile({ ...local, paused: true }, state({ version: 3 }))?.actions, [{ kind: "resume" }]);
  assert.deepEqual(reconcile(local, state({ version: 4, volume: 30 }))?.actions, [{ kind: "volume", volume: 30 }]);
});

test("another entry plays, even of the same song, and an empty queue stops", () => {
  const local = reconcile(IDLE, state())!.next;
  const next = state({ version: 2, nowPlaying: { entry: 2, url: "https://play.example/1", title: "One", uploader: "Band", requestedBy: "", thumbnail: "" } });
  assert.deepEqual(reconcile(local, next)?.actions, [{ kind: "play", entry: 2, url: "https://play.example/1" }]);
  assert.deepEqual(reconcile(local, state({ version: 3, nowPlaying: undefined }))?.actions, [{ kind: "stop" }]);
  assert.deepEqual(reconcile(IDLE, state({ nowPlaying: undefined }))?.actions, [], "nothing to stop");
});

test("a late reply is ignored, but a restarted orchestrator is not", () => {
  const local = reconcile(IDLE, state({ version: 5 }))!.next;
  assert.equal(reconcile(local, state({ version: 4, paused: true })), undefined);
  assert.equal(reconcile(local, state({ version: 5, paused: true })), undefined);
  const restarted = reconcile(local, state({ epoch: "e2", version: 1 }));
  assert.deepEqual(restarted?.actions, [{ kind: "play", entry: 1, url: "https://play.example/1" }],
    "entry 1 of a new epoch is another track than entry 1 of the old one");
});

test("only a well-formed reply is a state", () => {
  assert.equal(parseQueueState(undefined), undefined);
  assert.equal(parseQueueState({ ok: false, reason: "the queue is full" }), undefined);
  assert.equal(parseQueueState({ ok: true, version: 1 }), undefined, "no epoch");
  const parsed = parseQueueState({ ok: true, epoch: "e1", version: 2, paused: true, volume: 250, now_playing: { entry: 3, url: "file:///etc/passwd" } });
  assert.deepEqual(parsed, { epoch: "e1", version: 2, paused: true, volume: 100, waiting: 0 }, "only an http(s) page plays, and the volume is bounded");
  const full = parseQueueState({ ok: true, epoch: "e1", version: 3, waiting: [{}, {}], now_playing: { entry: 1, url: "https://play.example/1", requested_by: "12345", title: "One" } });
  assert.equal(full?.waiting, 2);
  assert.equal(full?.nowPlaying?.requestedBy, "12345");
  assert.equal(parseQueueState({ ok: true, epoch: "e1", version: 3, now_playing: { entry: 1, url: "https://play.example/1", requested_by: "<@everyone>" } })?.nowPlaying?.requestedBy, "", "only an id is mentioned");
});

test("a cover is kept only from an http(s) address", () => {
  const cover = (thumbnail: unknown) => parseQueueState({ ok: true, epoch: "e", version: 1, now_playing: { entry: 1, url: "https://play.example/1", thumbnail } })?.nowPlaying?.thumbnail;
  assert.equal(cover("https://covers.example/a.jpg"), "https://covers.example/a.jpg");
  assert.equal(cover("javascript:alert(1)"), "");
  assert.equal(cover(42), "");
});

test("the next track is read so it can be made ready, only from an http(s) page", () => {
  const parsed = parseQueueState({ ok: true, epoch: "e", version: 1, now_playing: { entry: 1, url: "https://play.example/1" },
    waiting: [{ entry: 2, url: "https://play.example/2" }, { entry: 3, url: "https://play.example/3" }] });
  assert.deepEqual(parsed?.next, { entry: 2, url: "https://play.example/2" });
  assert.equal(parseQueueState({ ok: true, epoch: "e", version: 1, waiting: [{ entry: 2, url: "file:///x" }] })?.next, undefined);
  assert.equal(parseQueueState({ ok: true, epoch: "e", version: 1, waiting: [] })?.next, undefined);
});
