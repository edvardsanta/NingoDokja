import { Events } from "discord.js";

// The closed list of states the interface reports. None of them carries an id of any kind.
export type DiscordGatewayState = "connecting" | "connected" | "disconnected";

export type InterfaceStatus = { status: "ok" | "degraded" | "error"; state: DiscordGatewayState; detail: string };

export type GatewayStateTracker = {
  get(): DiscordGatewayState;
  set(state: DiscordGatewayState): void;
};

export function createGatewayState(): GatewayStateTracker {
  let state: DiscordGatewayState = "connecting";
  return {
    get: () => state,
    set: (next) => {
      state = next;
    },
  };
}

// What the orchestrator reads to show this interface next to the services.
export function discordStatusReply(state: DiscordGatewayState): InterfaceStatus {
  const status = state === "connected" ? "ok" : state === "connecting" ? "degraded" : "error";
  return { status, state, detail: "" };
}

type Emitter = { on(event: string, listener: () => void): unknown };

// Follows the gateway of the client, so the state says whether the bot can hear messages and
// not just that the process is up. A shard that drops comes back by itself; until it does the
// bot is deaf.
export function trackGatewayState(client: Emitter, tracker: GatewayStateTracker): void {
  client.on(Events.ClientReady, () => tracker.set("connected"));
  client.on(Events.ShardReady, () => tracker.set("connected"));
  client.on(Events.ShardResume, () => tracker.set("connected"));
  client.on(Events.ShardReconnecting, () => tracker.set("connecting"));
  client.on(Events.ShardDisconnect, () => tracker.set("disconnected"));
  client.on(Events.Invalidated, () => tracker.set("disconnected"));
}
