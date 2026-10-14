import { buildConfig } from "./config.js";
import { connectSocket, loadAuth, newMessageId } from "./runtime/baileys_socket.js";
import { renderQr } from "./runtime/qr.js";
import { createWhatsAppRuntime } from "./runtime/whatsapp_runtime.js";

const log = (...args: unknown[]) => console.log("[dokja-whatsapp]", ...args);
const error = (...args: unknown[]) => console.error("[dokja-whatsapp]", ...args);

async function main() {
  const config = buildConfig();
  // Counts only: the lists hold phone numbers.
  log(JSON.stringify({
    bot: "starting",
    selfChat: config.selfChat,
    allowedNumbers: config.allowedNumbers.length,
    allowedGroups: config.allowedGroups.length,
  }));

  const runtime = createWhatsAppRuntime(config, {
    loadAuth,
    connect: (auth) => connectSocket(auth, error),
    renderQr: (qr) => renderQr(qr),
    newMessageId,
    log,
    error,
  });
  for (const signal of ["SIGINT", "SIGTERM"] as const) {
    process.once(signal, () => runtime.stop());
  }

  await runtime.run();
  process.exit(0);
}

void main().catch((reason: unknown) => {
  error("fatal", reason instanceof Error ? reason.message : reason);
  process.exit(1);
});
