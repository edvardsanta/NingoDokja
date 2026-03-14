export type DiscordInterfaceConfig = {
  applicationId?: string;
  allowedGuildId: string;
  allowedChannelIds: string[];
  dmPolicy: string;
  mode: "full" | "voice";
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  deliveryPort: number;
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
    orchestratorTimeoutMs: Number(env("OPENCLAW_ORCHESTRATOR_TIMEOUT_MS", "180000")),
    deliveryPort: Number(env("DOKJA_DISCORD_DELIVERY_PORT", "8092")),
  };
}
