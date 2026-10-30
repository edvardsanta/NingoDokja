import { join } from "node:path";

import { pathToFileURL } from "node:url";

import { app, dialog, ipcMain, net, protocol, session, shell, type BrowserWindow } from "electron";

import { isRecord } from "../shared/records.js";
import {
  CHANNELS,
  type Bootstrap,
  type PreviewResult,
  type TransportResult,
} from "../shared/transport.js";
import { RuntimeClient, runtimeRoot, runtimeStateHome } from "./runtime_client.js";
import { DEFAULT_ENDPOINT } from "./config.js";
import { localPostMediaOrigin, parseConfig } from "./config.js";
import { communicationsToken } from "./communications_config.js";
import { isTrustedSender } from "./guards.js";
import { downloadMedia, fetchMedia } from "./media_fetch.js";
import { POST_SCHEME } from "../shared/posts.js";
import { usePostStore } from "./actions.js";
import { PostStore } from "./post_store.js";
import { createTranslator } from "../renderer/i18n/i18n.js";
import { MediaGate } from "./media_gate.js";
import { createPreviewer } from "./media_preview.js";
import { BridgeChat, BridgeMissing } from "./chat_stream.js";
import { handleRequest, isAllowedAction } from "./ipc.js";
import { OrchestratorClient } from "./orchestrator_client.js";
import { installPermissionPolicy } from "./permissions.js";
import { createPresence } from "./presence.js";
import { createConfirmer } from "./dialogs.js";
import { appPage, blockRemoteRequests, createWindow } from "./window.js";

const LOG = "[dokja-desktop]";

// A post's files are shown from the shell's own scheme, which must be declared before the app is
// ready. It is secure and streams, so a video can seek.
protocol.registerSchemesAsPrivileged([
  { scheme: POST_SCHEME, privileges: { standard: true, secure: true, stream: true, supportFetchAPI: true } },
]);

async function main(): Promise<void> {
  await app.whenReady();

  // Flags win over the environment: --request-endpoint, --lang and --timeout (see the README).
  const config = parseConfig(process.argv.slice(1), process.env, app.getLocale());
  const distDir = join(__dirname, "..");
  const page = appPage(config, distDir);

  installPermissionPolicy(session.defaultSession);
  blockRemoteRequests(session.defaultSession, config.devServerUrl);

  const root = runtimeRoot(config.runtimeRoot ?? process.cwd());
  const postMediaOrigin = config.postMediaOrigin ?? localPostMediaOrigin(root);
  const orchestrator = new OrchestratorClient({ endpoint: config.endpoint,
    communicationsToken: communicationsToken(process.env, root, config.envFile) });
  const runtimeEnabled = config.endpoint === DEFAULT_ENDPOINT && root !== undefined && process.platform !== "win32";
  const runtime = new RuntimeClient({ root, enabled:runtimeEnabled, stateHome:runtimeStateHome(process.env, app.getPath("home")), legacyStateHome:app.getPath("userData"), binary:join(distDir,"runtime","dokja-runtime") });
  // A Talk turn is streamed through the orchestrator's bridge when its address and credential are set;
  // otherwise it goes over the request socket like every other request.
  const bridgeToken = process.env.DOKJA_GATEWAY_INGRESS_TOKEN?.trim() ?? "";
  const bridge = config.bridgeEndpoint && bridgeToken ? new BridgeChat({ endpoint: config.bridgeEndpoint, token: bridgeToken }) : undefined;
  const client = {
    request:(type:string,payload:Record<string,unknown>,timeout:number) =>
      type.startsWith("runtime.") ? runtime.request(type,payload,timeout) : orchestrator.request(type,payload,timeout),
    // A bridge that never saw the turn (not listening, or too old to stream) leaves it to the socket.
    ...(bridge ? { streamChat:(payload:Record<string,unknown>,timeout:number,onPreview:(text:string)=>void) =>
      bridge.stream(payload,timeout,onPreview).catch((error:unknown) => {
        if (error instanceof BridgeMissing) return orchestrator.request("message.created",payload,timeout);
        throw error;
      }) } : {}),
  };
  const limits = { defaultTimeoutMs: config.defaultTimeoutMs, maxTimeoutMs: config.maxTimeoutMs };
  const untrusted = { ok: false, error: { code: "denied", message: "this page is not the app" } } as const;
  const media = new MediaGate();
  const presence = createPresence();
  let window: BrowserWindow | undefined;
  // The question for an action that cannot be undone, in a window of the shell's own. Cancel is
  // the default, so Enter or Escape never deletes.
  const confirm = createConfirmer(config.locale, async (question) => {
    if (!window) return false;
    const { response } = await dialog.showMessageBox(window, {
      type: "warning",
      message: question.message,
      detail: question.detail,
      buttons: [question.accept, question.cancel],
      defaultId: 1,
      cancelId: 1,
      noLink: true,
    });
    return response === 0;
  });
  const preview = createPreviewer({ gate: media, fetchMedia: (url) => fetchMedia(url) });

  // Posts opened in this run live in a temporary folder, erased when the app quits; a saved post is
  // copied to the posts folder, which stays.
  const posts = new PostStore({
    savedRoot: config.postsDir ?? join(app.getPath("userData"), "saved-posts"),
    download: downloadMedia,
    translate: createTranslator(config.locale),
    // The owner picks the folder in the system's own window; closing it saves nothing.
    pickFolder: async (defaultPath) => {
      if (!window) return undefined;
      const t = createTranslator(config.locale);
      const { canceled, filePaths } = await dialog.showOpenDialog(window, {
        title: t("post_pick_title"),
        message: t("post_pick_title"),
        buttonLabel: t("post_pick_button"),
        defaultPath,
        properties: ["openDirectory", "createDirectory"],
      });
      return canceled ? undefined : filePaths[0];
    },
    ...(postMediaOrigin ? { localOrigin: postMediaOrigin } : {}),
  });
  usePostStore(posts);
  void posts.sweep();
  session.defaultSession.protocol.handle(POST_SCHEME, (request) => {
    const file = posts.file(request.url);
    if (!file) return new Response(null, { status: 404 });
    return net.fetch(pathToFileURL(file.path).href, { headers: request.headers });
  });
  let disposed = false;
  app.on("will-quit", (event) => {
    if (disposed) return;
    event.preventDefault();
    disposed = true;
    void posts.dispose().finally(() => app.quit());
  });

  ipcMain.handle(CHANNELS.bootstrap, (event): Bootstrap | null =>
    isTrustedSender(event.senderFrame?.url, page) ? { locale: config.locale, runtimeControls: runtimeEnabled } : null,
  );

  ipcMain.handle(CHANNELS.request, async (event, raw: unknown): Promise<TransportResult> => {
    if (!isTrustedSender(event.senderFrame?.url, page)) return untrusted;

    const started = Date.now();
    const result = await handleRequest(raw, client, limits, media, presence, confirm, (preview) => {
      if (!event.sender.isDestroyed()) event.sender.send(CHANNELS.chatPreview, preview);
    });
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

  // Opens a saved post's folder in the file manager: only one saved in this run, only on a real press.
  ipcMain.handle(CHANNELS.openSavedPost, async (event, documentId: unknown): Promise<TransportResult> => {
    if (!isTrustedSender(event.senderFrame?.url, page)) return untrusted;
    if (!presence.recent()) return { ok: false, error: { code: "denied", message: "opening a folder needs a click or a key press just before it" } };
    const folder = typeof documentId === "string" ? await posts.savedFolder(documentId) : undefined;
    if (!folder) return { ok: false, error: { code: "denied", message: "that post was not saved in this run" } };
    const failed = await shell.openPath(folder);
    return failed ? { ok: false, error: { code: "unavailable", message: "the folder could not be opened" } } : { ok: true, result: { opened: true } };
  });

  console.log(LOG, "orchestrator", JSON.stringify({ endpoint: config.endpoint, locale: config.locale }));
  window = createWindow(page, join(__dirname, "..", "preload", "preload.cjs"));
  // What the browser reports as input, which a script in the page cannot produce, is what lets a
  // change through (see presence.ts).
  window.webContents.on("input-event", (_event, input) => presence.note(input.type));

  app.on("window-all-closed", () => app.quit());
}

main().catch((error: unknown) => {
  console.error(LOG, "failed to start", error);
  app.exit(1);
});
