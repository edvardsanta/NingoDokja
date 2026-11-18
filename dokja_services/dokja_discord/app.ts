import { closeDeliveryServer, type Shutdown } from "./runtime/shutdown.js";
import type { DiscordInterfaceConfig } from "./config.js";
import { createMusic } from "./music/commands.js";
import { createCommands } from "./commands/commands.js";
import { createInteractionDefinitions } from "./commands/interactions.js";
import { startDiscordDeliveryServer } from "./delivery/delivery_server.js";
import { createDiscordMessageHandler } from "./messages/message_listener.js";
import { openDeferredDispatchStore, type DeferredDispatchStore } from "./messages/deferred_dispatch.js";
import { startDiscordJsRuntime, toMessageReply } from "./runtime/discordjs_runtime.js";
import { createGatewayState, discordStatusReply } from "./runtime/gateway_state.js";
import { createDiscordVoiceController } from "./voice/voice.js";
import type { Client } from "discord.js";
import type { DiscordReply } from "./types.js";

export async function runDiscordApp(token: string, cfg: DiscordInterfaceConfig, shutdown: Shutdown): Promise<void> {
  const runtime = {
    log: (...args: unknown[]) => console.log("[dokja-discord]", ...args),
    error: (...args: unknown[]) => console.error("[dokja-discord]", ...args),
  };

  const gateway = createGatewayState();

  const voice = createDiscordVoiceController({
    token,
    voiceServiceEndpoint: cfg.voiceServiceEndpoint,
    orchestratorEndpoint: cfg.orchestratorEndpoint,
    logTranscripts: cfg.mode === "full",
    log: runtime.log,
    error: runtime.error,
  });
  shutdown.add(() => voice.dispose?.());
  const MUSIC_COMMANDS = new Set(["play", "queue"]);
  // One music feature: its commands and its panel buttons share the queue they play.
  const music = createMusic({
    orchestratorEndpoint: cfg.orchestratorEndpoint,
    orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
    voice,
    locale: cfg.locale,
  });
  shutdown.add(() => music.dispose?.());
  const allCommands = createCommands(cfg.orchestratorEndpoint, cfg.orchestratorTimeoutMs, voice, {
    logVoiceConversation: cfg.mode === "full",
    locale: cfg.locale,
    music,
  });
  const commands =
    cfg.mode === "voice"
      ? allCommands.filter(
          (entry) =>
            entry.name.startsWith("conversation_") ||
            entry.name === "speak" ||
            entry.name === "voice_chat_start" ||
            entry.name === "voice_chat_stop" ||
            entry.name === "play_radio" ||
            entry.name === "stop_radio" ||
            MUSIC_COMMANDS.has(entry.name),
        )
      : allCommands;

  runtime.log?.(
    JSON.stringify({
      mode: cfg.mode,
      orchestratorUrl: cfg.orchestratorEndpoint,
      voiceServiceUrl: cfg.voiceServiceEndpoint,
      guildIds: cfg.allowedGuildIds.join(","),
      allowedChannels: cfg.allowedChannelIds.join(","),
      dmPolicy: cfg.dmPolicy,
      commandDeploy: cfg.allowedGuildIds.length > 0 ? "guild" : "global",
      commands: commands.map((entry) => entry.name),
      deliveryPort: cfg.deliveryPort,
      deliveryWebhookChannels: Object.keys(cfg.deliveryWebhooks),
    }),
  );

  if (cfg.mode === "full") {
    const server = startDiscordDeliveryServer({
      token,
      port: cfg.deliveryPort,
      webhooks: cfg.deliveryWebhooks,
      status: () => discordStatusReply(gateway.get()),
      log: runtime.log,
      error: runtime.error,
    });
    shutdown.add(() => closeDeliveryServer(server));
  }

  // Late retries answer the original message after a restart, when only its ids
  // remain: the discord.js client captured at startup fetches that message and
  // replies to it, the same rendering a live context.reply uses.
  let discordClient: Client | undefined;
  const lateReply = async (target: { channelId: string; messageId: string }, reply: DiscordReply) => {
    if (!discordClient) throw new Error("discord client is not connected");
    const channel = await discordClient.channels.fetch(target.channelId);
    if (!channel || !channel.isTextBased()) throw new Error("channel is not text-based");
    const original = await channel.messages.fetch(target.messageId);
    await original.reply(toMessageReply(reply));
  };

  // The deferred-dispatch queue persists beside the runtime databases: a promised
  // late retry survives a container restart and is re-armed on boot.
  let deferralStore: DeferredDispatchStore | undefined;
  if (cfg.mode === "full") {
    deferralStore = openDeferredDispatchStore(cfg.deferralDbPath);
    shutdown.add(() => deferralStore?.close());
  }
  const messageHandling = cfg.mode === "full"
    ? createDiscordMessageHandler({
        orchestratorEndpoint: cfg.orchestratorEndpoint,
        orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
        locale: cfg.locale,
        ...(deferralStore ? { deferralStore } : {}),
        lateReply,
      })
    : undefined;

  const interactions = cfg.mode === "full"
    ? createInteractionDefinitions({
        orchestratorEndpoint: cfg.orchestratorEndpoint,
        orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
        locale: cfg.locale,
        voice,
      })
    : { buttons: [], selects: [], modals: [] };

  await startDiscordJsRuntime({
    signal: shutdown.signal,
    onClientCreated: (client) => {
      discordClient = client;
      shutdown.add(() => client.destroy());
    },
    token,
    applicationId: cfg.applicationId,
    devGuildIds: cfg.allowedGuildIds,
    log: runtime.log,
    error: runtime.error,
    commands,
    gateway,
    buttons: [...interactions.buttons, ...music.buttons],
    selects: interactions.selects,
    modals: interactions.modals,
    onReady: (client) => {
      voice.attachClient(client);
    },
    onMessage: messageHandling?.handler ?? (async () => {}),
  });
  // Re-arm promised late retries only once the gateway connection is up: a due
  // job may fire immediately, and its answer needs the client to be connected.
  messageHandling?.restoreDeferrals();
}
