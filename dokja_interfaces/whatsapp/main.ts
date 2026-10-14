import { buildConfig } from "./config.js";
import { connectSocket, loadAuth, newMessageId, wipeAuth } from "./runtime/baileys_socket.js";
import { HistoryStore } from "./history/store.js";
import { createDeskSender } from "./messages/desk_sender.js";
import { createRecorder } from "./messages/recorder.js";
import { SendLimit } from "./runtime/send_limit.js";
import { renderQr } from "./runtime/qr.js";
import { startStatusServer } from "./runtime/status_server.js";
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
    conversations: config.conversations.length,
  }));

  // Only the conversations the owner named are ever recorded; with none named there is no store at all.
  const store =
    config.conversations.length > 0
      ? new HistoryStore({ dir: config.historyDir, retentionMs: config.historyDays * 24 * 60 * 60 * 1000 })
      : undefined;
  if (store) {
    await store.init();
    // Past the retention is forgotten at start-up and once a day after that.
    setInterval(() => void store.purge().catch(() => undefined), 24 * 60 * 60 * 1000).unref();
  }

  const runtime = createWhatsAppRuntime(config, {
    recorder: store ? createRecorder(config.conversations, store, { error }) : undefined,
    loadAuth,
    wipeAuth,
    connect: (auth) => connectSocket(auth, error),
    renderQr: (qr) => renderQr(qr),
    newMessageId,
    log,
    error,
  });
  startStatusServer({
    port: config.statusPort,
    status: () => runtime.status(),
    link: { code: () => runtime.linkCode(), start: () => runtime.restart(), reset: () => runtime.resetSession() },
    deliver: store
      ? createDeskSender({
          conversations: config.conversations,
          send: (jid, text) => runtime.sendText(jid, text),
          connected: () => runtime.status().state === "connected",
          limit: new SendLimit(),
          record: (alias, text, id) =>
            store.append(alias, { id: id ?? `out-${Date.now()}`, at: Date.now(), who: "owner", text }),
        })
      : undefined,
    conversations: store
      ? {
          list: () => config.conversations.map((entry) => ({ alias: entry.alias, kind: entry.group ? "group" : "direct" })),
          retentionDays: config.historyDays,
          page: (alias, before) => store.page(alias, before),
        }
      : undefined,
    log,
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
