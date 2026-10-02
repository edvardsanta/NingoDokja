import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { BrowserWindow, type Session } from "electron";

import type { DesktopConfig } from "./config.js";
import { isSameDocument, shouldBlockRequest } from "./guards.js";

// The page background (--static in styles.css), so the window is never white while it loads.
const BACKGROUND = "#101b25";

// The page the window shows: the development server when one is configured, else the built page.
export function appPage(config: DesktopConfig, distDir: string): string {
  return config.devServerUrl ?? pathToFileURL(join(distDir, "renderer", "index.html")).href;
}

export function blockRemoteRequests(session: Session, devServerUrl?: string): void {
  session.webRequest.onBeforeRequest((details, callback) => {
    callback({ cancel: shouldBlockRequest(details.url, devServerUrl) });
  });
}

export function createWindow(page: string, preloadPath: string): BrowserWindow {
  const window = new BrowserWindow({
    width: 1100,
    height: 800,
    title: "Ningo",
    show: false,
    autoHideMenuBar: true,
    backgroundColor: BACKGROUND,
    webPreferences: {
      preload: preloadPath,
      contextIsolation: true,
      sandbox: true,
      nodeIntegration: false,
      webSecurity: true,
      allowRunningInsecureContent: false,
      webviewTag: false,
      navigateOnDragDrop: false,
    },
  });

  // A page that fails to load is still shown, so the failure is visible and not a missing window.
  window.once("ready-to-show", () => window.show());
  window.webContents.on("did-fail-load", (_event, code, description) => {
    console.error("[dokja-desktop] page failed to load", JSON.stringify({ code, description }));
    window.show();
  });

  // The window shows one page and nothing else: no navigation away, no new windows.
  window.webContents.on("will-navigate", (event, url) => {
    if (!isSameDocument(url, page)) event.preventDefault();
  });
  window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));

  void window.loadURL(page);
  return window;
}
