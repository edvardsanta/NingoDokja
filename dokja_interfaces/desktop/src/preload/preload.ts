import { contextBridge, ipcRenderer } from "electron";

import type { ActionType } from "../shared/actions.js";
import {
  CHANNELS,
  type Bootstrap,
  type ChatPreview,
  type PreviewResult,
  type RequestOptions,
  type TransportResult,
} from "../shared/transport.js";

// All the page can reach: four functions. It gets no Node, no ipcRenderer and no other channel; the
// fourth only listens, and what it hears is reduced to two strings.
contextBridge.exposeInMainWorld("dokja", {
  bootstrap: (): Promise<Bootstrap | null> => ipcRenderer.invoke(CHANNELS.bootstrap),
  request: (
    type: ActionType,
    payload?: Record<string, unknown>,
    options?: RequestOptions,
  ): Promise<TransportResult> => ipcRenderer.invoke(CHANNELS.request, { type, payload, options }),
  preview: (url: string): Promise<PreviewResult> => ipcRenderer.invoke(CHANNELS.preview, url),
  onChatPreview: (listener: (preview: ChatPreview) => void): (() => void) => {
    const hear = (_event: unknown, raw: unknown) => {
      const value = (raw ?? {}) as { conversationId?: unknown; text?: unknown };
      if (typeof value.conversationId === "string" && typeof value.text === "string") listener({ conversationId: value.conversationId, text: value.text });
    };
    ipcRenderer.on(CHANNELS.chatPreview, hear);
    return () => {
      ipcRenderer.removeListener(CHANNELS.chatPreview, hear);
    };
  },
});
