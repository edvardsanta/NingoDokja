import type { ActionType } from "./actions.js";
import type { Locale } from "./locale.js";

export type TransportErrorCode =
  | "denied" // the action is not on the allow-list
  | "invalid" // the request itself is malformed
  | "unavailable" // nobody accepted the request: the orchestrator is not running or not reachable
  | "timeout" // no answer before the deadline
  | "orchestrator" // the orchestrator answered with an error
  | "unexpected"; // the reply was not what the action expects

export type TransportError = { code: TransportErrorCode; message: string };

// Errors cross the IPC boundary as data, because an Error loses its class there.
export type TransportResult =
  | { ok: true; result: unknown }
  | { ok: false; error: TransportError };

export type RequestOptions = { timeoutMs?: number };

// The only door between the screen and the orchestrator. In Electron the preload script
// implements it over IPC; in a browser (development and tests) a hand-built fake does.
export type Transport = {
  request(
    type: ActionType,
    payload?: Record<string, unknown>,
    options?: RequestOptions,
  ): Promise<TransportResult>;
};

export type Bootstrap = { locale: Locale };

export const CHANNELS = {
  bootstrap: "dokja:bootstrap",
  request: "dokja:request",
} as const;
