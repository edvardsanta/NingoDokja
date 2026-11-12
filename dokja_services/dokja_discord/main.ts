import { runDiscordApp } from "./app.js";
import { buildConfig, env } from "./config.js";

import { createShutdown } from "./runtime/shutdown.js";

const shutdown = createShutdown({
  exit: (code) => process.exit(code),
  error: (message) => console.error("[dokja-discord]", message),
});
process.on("SIGTERM", () => { void shutdown.stop(); });
process.on("SIGINT", () => { void shutdown.stop(); });

async function main() {
  const token = env("DISCORD_BOT_TOKEN");
  if (!token) {
    throw new Error("DISCORD_BOT_TOKEN is required");
  }

  await runDiscordApp(token, buildConfig(), shutdown);
}

void main().catch(async (error: unknown) => {
  if (!shutdown.signal.aborted) console.error("[dokja-discord] fatal", error);
  await shutdown.stop(shutdown.signal.aborted ? 0 : 1);
});
