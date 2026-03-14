import type { DiscordInterfaceConfig } from "./config.js";
import { createCommands } from "./commands/commands.js";
import { createInteractionDefinitions } from "./commands/interactions.js";
import { startDiscordDeliveryServer } from "./delivery/delivery_server.js";
import { createDiscordMessageHandler } from "./messages/message_listener.js";
import { startDiscordJsRuntime } from "./runtime/discordjs_runtime.js";
import { createDiscordVoiceController } from "./voice/voice.js";

export async function runDiscordApp(token: string, cfg: DiscordInterfaceConfig): Promise<void> {
  const runtime = {
    log: (...args: unknown[]) => console.log("[dokja-discord]", ...args),
    error: (...args: unknown[]) => console.error("[dokja-discord]", ...args),
  };

  const voice = createDiscordVoiceController({
    token,
    log: runtime.log,
    error: runtime.error,
  });
  const allCommands = createCommands(cfg.orchestratorEndpoint, cfg.orchestratorTimeoutMs, voice);
  const commands =
    cfg.mode === "voice"
      ? allCommands.filter((entry) => entry.name === "play_radio" || entry.name === "stop_radio")
      : allCommands;

  runtime.log?.(
    JSON.stringify({
      mode: cfg.mode,
      orchestratorUrl: cfg.orchestratorEndpoint,
      guildId: cfg.allowedGuildId,
      allowedChannels: cfg.allowedChannelIds.join(","),
      dmPolicy: cfg.dmPolicy,
      commandDeploy: cfg.allowedGuildId ? "guild" : "global",
      commands: commands.map((entry) => entry.name),
      deliveryPort: cfg.deliveryPort,
    }),
  );

  if (cfg.mode === "full") {
    startDiscordDeliveryServer({
      token,
      port: cfg.deliveryPort,
      log: runtime.log,
      error: runtime.error,
    });
  }

  const interactions = cfg.mode === "full"
    ? createInteractionDefinitions({
        orchestratorEndpoint: cfg.orchestratorEndpoint,
        orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
        voice,
      })
    : { buttons: [], selects: [], modals: [] };

  await startDiscordJsRuntime({
    token,
    applicationId: cfg.applicationId,
    devGuildId: cfg.allowedGuildId,
    log: runtime.log,
    error: runtime.error,
    commands,
    buttons: interactions.buttons,
    selects: interactions.selects,
    modals: interactions.modals,
    onReady: (client) => {
      voice.attachClient(client);
    },
    onMessage: cfg.mode === "full"
      ? createDiscordMessageHandler({
          orchestratorEndpoint: cfg.orchestratorEndpoint,
          orchestratorTimeoutMs: cfg.orchestratorTimeoutMs,
          allowedGuildId: cfg.allowedGuildId,
          allowedChannelIds: cfg.allowedChannelIds,
          dmPolicy: cfg.dmPolicy,
        })
      : async () => {},
  });
}
