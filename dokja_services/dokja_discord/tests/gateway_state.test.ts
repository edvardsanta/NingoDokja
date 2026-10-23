import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import type { AddressInfo } from "node:net";
import test from "node:test";

import { Events } from "discord.js";

import { startDiscordDeliveryServer } from "../delivery/delivery_server.js";
import { createGatewayState, discordStatusReply, trackGatewayState } from "../runtime/gateway_state.js";

test("a new tracker is connecting and keeps what it is told", () => {
  const tracker = createGatewayState();
  assert.equal(tracker.get(), "connecting");

  tracker.set("connected");

  assert.equal(tracker.get(), "connected");
});

test("each state maps to a level and carries nothing else", () => {
  assert.deepEqual(discordStatusReply("connected"), { status: "ok", state: "connected", detail: "" });
  assert.deepEqual(discordStatusReply("connecting"), { status: "degraded", state: "connecting", detail: "" });
  assert.deepEqual(discordStatusReply("disconnected"), { status: "error", state: "disconnected", detail: "" });
});

test("the gateway events move the state, so a dropped shard is not reported as up", () => {
  const client = new EventEmitter();
  const tracker = createGatewayState();
  trackGatewayState(client, tracker);

  client.emit(Events.ClientReady);
  assert.equal(tracker.get(), "connected");
  client.emit(Events.ShardDisconnect);
  assert.equal(tracker.get(), "disconnected");
  client.emit(Events.ShardReconnecting);
  assert.equal(tracker.get(), "connecting");
  client.emit(Events.ShardResume);
  assert.equal(tracker.get(), "connected");
  client.emit(Events.Invalidated);
  assert.equal(tracker.get(), "disconnected");
  client.emit(Events.ShardReady);
  assert.equal(tracker.get(), "connected");
});

test("the delivery listener answers the state on /status and still refuses other paths", async () => {
  const tracker = createGatewayState();
  tracker.set("connected");
  const server = startDiscordDeliveryServer({
    token: "unused",
    port: 0,
    status: () => discordStatusReply(tracker.get()),
  });
  await new Promise<void>((resolve) => server.once("listening", resolve));
  const { port } = server.address() as AddressInfo;
  const base = `http://127.0.0.1:${port}`;

  try {
    assert.deepEqual(await (await fetch(`${base}/status`)).json(), { status: "ok", state: "connected", detail: "" });
    tracker.set("disconnected");
    assert.equal((await (await fetch(`${base}/status`)).json() as { state: string }).state, "disconnected");
    assert.equal((await fetch(`${base}/other`)).status, 404);
  } finally {
    await new Promise((resolve) => server.close(resolve));
  }
});
