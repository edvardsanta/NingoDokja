import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import test from "node:test";
import { VoiceConnectionStatus, type VoiceConnection } from "@discordjs/voice";
import { ensureConnectionReady } from "../voice/connection_ready.js";

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
