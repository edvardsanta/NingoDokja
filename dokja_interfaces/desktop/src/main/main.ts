import { join } from "node:path";

import { app, ipcMain, session } from "electron";

import { isRecord } from "../shared/records.js";
import {
  CHANNELS,
  type Bootstrap,
  type PreviewResult,
  type TransportResult,
} from "../shared/transport.js";
import { parseConfig } from "./config.js";
import { isTrustedSender } from "./guards.js";
import { fetchMedia } from "./media_fetch.js";
import { MediaGate } from "./media_gate.js";
import { createPreviewer } from "./media_preview.js";
import { handleRequest, isAllowedAction } from "./ipc.js";
import { OrchestratorClient } from "./orchestrator_client.js";
import { installPermissionPolicy } from "./permissions.js";
import { createPresence } from "./presence.js";
import { appPage, blockRemoteRequests, createWindow } from "./window.js";

const LOG = "[dokja-desktop]";

async function main(): Promise<void> {
  await app.whenReady();

  // Flags win over the environment: --request-endpoint, --lang and --timeout (see the README).
  const config = parseConfig(process.argv.slice(1), process.env, app.getLocale());
  const distDir = join(__dirname, "..");
  const page = appPage(config, distDir);

  installPermissionPolicy(session.defaultSession);
  blockRemoteRequests(session.defaultSession, config.devServerUrl);

  const orchestrator = new OrchestratorClient({ endpoint: config.endpoint });
  const limits = { defaultTimeoutMs: config.defaultTimeoutMs, maxTimeoutMs: config.maxTimeoutMs };
  const untrusted = { ok: false, error: { code: "denied", message: "this page is not the app" } } as const;
  const media = new MediaGate();
  const presence = createPresence();
  const preview = createPreviewer({ gate: media, fetchMedia: (url) => fetchMedia(url) });

  ipcMain.handle(CHANNELS.bootstrap, (event): Bootstrap | null =>
    isTrustedSender(event.senderFrame?.url, page) ? { locale: config.locale } : null,
  );

  ipcMain.handle(CHANNELS.request, async (event, raw: unknown): Promise<TransportResult> => {
    if (!isTrustedSender(event.senderFrame?.url, page)) return untrusted;

    const started = Date.now();
    const result = await handleRequest(raw, orchestrator, limits, media, presence);
    // Only the action name and the outcome are logged, never a payload or a reply.
    const type = isRecord(raw) && isAllowedAction(raw.type) ? raw.type : "(refused)";
    console.log(
      LOG,
      "request",
      JSON.stringify({
        type,
        ok: result.ok,
        code: result.ok ? undefined : result.error.code,
        ms: Date.now() - started,
      }),
    );
    return result;
  });

  ipcMain.handle(CHANNELS.preview, async (event, url: unknown): Promise<PreviewResult> => {
    if (!isTrustedSender(event.senderFrame?.url, page)) return untrusted;

    const started = Date.now();
    const result = await preview(url);
    // The address is never logged, only the outcome.
    console.log(
      LOG,
      "preview",
      JSON.stringify({
        ok: result.ok,
        code: result.ok ? undefined : result.error.code,
        ms: Date.now() - started,
      }),
    );
    return result;
  });

  console.log(LOG, "orchestrator", JSON.stringify({ endpoint: config.endpoint, locale: config.locale }));
  const window = createWindow(page, join(__dirname, "..", "preload", "preload.cjs"));
  // What the browser reports as input, which a script in the page cannot produce, is what lets a
  // change through (see presence.ts).
  window.webContents.on("input-event", (_event, input) => presence.note(input.type));

  app.on("window-all-closed", () => app.quit());
}

main().catch((error: unknown) => {
  console.error(LOG, "failed to start", error);
  app.exit(1);
});
