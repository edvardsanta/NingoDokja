import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import { VoiceConnectionStatus, type VoiceConnection } from "@discordjs/voice";
import { ensureConnectionReady, watchConnectionLoss } from "../voice/connection_ready.js";

class Connection extends EventEmitter {
  state: { status: VoiceConnectionStatus } = { status: VoiceConnectionStatus.Disconnected };
  joins = 0;
  accepts = true;
  target?: string;
  rejoin(config: { channelId: string }) { this.joins++; this.target = config.channelId; return this.accepts; }
  ready() { this.state.status = VoiceConnectionStatus.Ready; this.emit(VoiceConnectionStatus.Ready); }
}

test("a disconnected connection rejoins and blocks playback until Ready", async () => {
  const connection = new Connection();
  let playback = false;
  const pending = ensureConnectionReady(connection as unknown as VoiceConnection, "voice1", 1000)
    .then(() => { playback = true; });
  assert.equal(connection.joins, 1);
  assert.equal(connection.target, "voice1");
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(playback, false);
  connection.ready();
  await pending;
  assert.equal(playback, true);
});

test("a healthy connection is reused without another join", async () => {
  const connection = new Connection();
  connection.ready();
  await ensureConnectionReady(connection as unknown as VoiceConnection, "voice1", 100);
  assert.equal(connection.joins, 0);
});

test("a connecting connection waits without interrupting its handshake", async () => {
  const connection = new Connection();
  connection.state.status = VoiceConnectionStatus.Connecting;
  const pending = ensureConnectionReady(connection as unknown as VoiceConnection, "voice1", 1000);
  assert.equal(connection.joins, 0);
  connection.ready();
  await pending;
});

test("failed rejoin and readiness timeout reject playback", async () => {
  const refused = new Connection();
  refused.accepts = false;
  await assert.rejects(ensureConnectionReady(refused as unknown as VoiceConnection, "voice1", 100), /could not rejoin/);
  const stalled = new Connection();
  await assert.rejects(ensureConnectionReady(stalled as unknown as VoiceConnection, "voice1", 20));
  assert.equal(stalled.joins, 1);
});


test("a persistent disconnect is detected without another playback request", t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const connection = new Connection();
  connection.ready();
  let lost = 0;
  watchConnectionLoss(connection as unknown as VoiceConnection, () => { lost++; });
  connection.state.status = VoiceConnectionStatus.Disconnected;
  connection.emit("stateChange");
  connection.emit("stateChange");
  t.mock.timers.tick(4999);
  assert.equal(lost, 0);
  t.mock.timers.tick(1);
  assert.equal(lost, 1);
  connection.emit("stateChange");
  t.mock.timers.tick(5000);
  assert.equal(lost, 1, "one queue-stop notification per lost connection");
  assert.equal(connection.joins, 0, "detection does not force the bot back into a call");
});

test("recovery cancels loss detection, but another disconnect starts a new grace period", t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  const connection = new Connection();
  let lost = 0;
  watchConnectionLoss(connection as unknown as VoiceConnection, () => { lost++; });
  t.mock.timers.tick(1000);
  connection.ready();
  connection.emit("stateChange");
  t.mock.timers.tick(5000);
  assert.equal(lost, 0);
  connection.state.status = VoiceConnectionStatus.Disconnected;
  connection.emit("stateChange");
  connection.state.status = VoiceConnectionStatus.Connecting;
  connection.emit("stateChange");
  t.mock.timers.tick(5000);
  assert.equal(lost, 1, "an unfinished handshake cannot retain stale playback indefinitely");
});

test("destroying or disposing a connection cancels pending loss detection", t => {
  t.mock.timers.enable({ apis: ["setTimeout"] });
  for (const destroyed of [true, false]) {
    const connection = new Connection();
    let lost = 0;
    const dispose = watchConnectionLoss(connection as unknown as VoiceConnection, () => { lost++; });
    if (destroyed) {
      connection.state.status = VoiceConnectionStatus.Destroyed;
      connection.emit("stateChange");
    } else dispose();
    t.mock.timers.tick(5000);
    assert.equal(lost, 0);
    assert.equal(connection.listenerCount("stateChange"), 0);
  }
});
