export type DiscordInterfaceConfig = {
  applicationId?: string;
  allowedGuildId: string;
  allowedChannelIds: string[];
  dmPolicy: string;
  mode: "full" | "voice";
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  voiceServiceEndpoint: string;
  deliveryPort: number;
  deliveryWebhooks: Record<string, string>;
};

export function env(name: string, fallback = ""): string {
  const value = process.env[name]?.trim();
  return value && value.length > 0 ? value : fallback;
}

export function buildConfig(): DiscordInterfaceConfig {
  return {
    applicationId: env("DISCORD_APPLICATION_ID") || undefined,
    allowedGuildId: env("DISCORD_GUILD_ID"),
    allowedChannelIds: env("DISCORD_ALLOWED_CHANNELS")
      .split(",")
      .map((value) => value.trim())
      .filter(Boolean),
    dmPolicy: env("DISCORD_DM_POLICY", "open"),
    mode: env("DOKJA_DISCORD_MODE", "full") === "voice" ? "voice" : "full",
    orchestratorEndpoint: env(
      "DOKJA_ORCH_HTTP_ENDPOINT",
      "http://dokja-orchestrator:8091/orchestrator",
    ),
    orchestratorTimeoutMs: Number(env("DOKJA_ORCHESTRATOR_TIMEOUT_MS", "180000")),
    voiceServiceEndpoint: env("DOKJA_VOICE_HTTP_ENDPOINT", "http://dokja-voice:8081"),
    deliveryPort: Number(env("DOKJA_DISCORD_DELIVERY_PORT", "8092")),
    deliveryWebhooks: parseWebhookMap(env("DOKJA_DISCORD_WEBHOOKS")),
  };
}

// Parses `channelId=webhookUrl,channelId2=webhookUrl2`. Splits on the first "="
// only, since webhook URLs may carry query strings.
export function parseWebhookMap(raw: string): Record<string, string> {
  const webhooks: Record<string, string> = {};
  for (const entry of raw.split(",")) {
    const separator = entry.indexOf("=");
    if (separator < 0) {
      continue;
    }
    const channelId = entry.slice(0, separator).trim();
    const url = entry.slice(separator + 1).trim();
    if (channelId && url) {
      webhooks[channelId] = url;
    }
  }
  return webhooks;
}
