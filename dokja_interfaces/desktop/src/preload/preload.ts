import { contextBridge, ipcRenderer } from "electron";

import type { ActionType } from "../shared/actions.js";
import {
  CHANNELS,
  type Bootstrap,
  type RequestOptions,
  type TransportResult,
} from "../shared/transport.js";

// All the page can reach: two functions. It gets no Node, no ipcRenderer and no other channel.
contextBridge.exposeInMainWorld("dokja", {
  bootstrap: (): Promise<Bootstrap | null> => ipcRenderer.invoke(CHANNELS.bootstrap),
  request: (
    type: ActionType,
    payload?: Record<string, unknown>,
    options?: RequestOptions,
  ): Promise<TransportResult> => ipcRenderer.invoke(CHANNELS.request, { type, payload, options }),
});
