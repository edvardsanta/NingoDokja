import type { DiscordInterfaceConfig } from "./config.js";
import { createMusic } from "./music/commands.js";
import { createCommands } from "./commands/commands.js";
import { createInteractionDefinitions } from "./commands/interactions.js";
import { startDiscordDeliveryServer } from "./delivery/delivery_server.js";
import { createDiscordMessageHandler } from "./messages/message_listener.js";
import { startDiscordJsRuntime } from "./runtime/discordjs_runtime.js";
import { createGatewayState, discordStatusReply } from "./runtime/gateway_state.js";
import { createDiscordVoiceController } from "./voice/voice.js";

export async function runDiscordApp(token: string, cfg: DiscordInterfaceConfig): Promise<void> {
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
  const MUSIC_COMMANDS = new Set(["play", "skip", "pause", "resume", "stop", "queue", "volume"]);
  // One music feature: its commands and its panel buttons share the queue they play.
  const music = createMusic({
    orchestratorEndpoint: cfg.orchestratorEndpoint,
    orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
    voice,
    locale: cfg.locale,
  });
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
            entry.name === "wake_chat" ||
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
    startDiscordDeliveryServer({
      token,
      port: cfg.deliveryPort,
      webhooks: cfg.deliveryWebhooks,
      status: () => discordStatusReply(gateway.get()),
      log: runtime.log,
      error: runtime.error,
    });
  }

  const interactions = cfg.mode === "full"
    ? createInteractionDefinitions({
        orchestratorEndpoint: cfg.orchestratorEndpoint,
        orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
        locale: cfg.locale,
        voice,
      })
    : { buttons: [], selects: [], modals: [] };

  await startDiscordJsRuntime({
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
    onMessage: cfg.mode === "full"
      ? createDiscordMessageHandler({
          orchestratorEndpoint: cfg.orchestratorEndpoint,
          orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
        })
      : async () => {},
  });
}
