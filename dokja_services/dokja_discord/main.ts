import { runDiscordApp } from "./app.js";
import { buildConfig, env } from "./config.js";

async function main() {
  const token = env("DISCORD_BOT_TOKEN");
  if (!token) {
    throw new Error("DISCORD_BOT_TOKEN is required");
  }

  await runDiscordApp(token, buildConfig());
}

void main().catch((error: unknown) => {
  console.error("[dokja-discord] fatal", error);
  process.exit(1);
});
