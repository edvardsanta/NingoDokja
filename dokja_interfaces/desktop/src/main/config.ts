import { parseArgs } from "node:util";

import { localeFromTag, type Locale } from "../shared/locale.js";

export const DEFAULT_ENDPOINT = "tcp://127.0.0.1:5558";
export const DEFAULT_REQUEST_TIMEOUT_MS = 15_000;
export const DEFAULT_MAX_TIMEOUT_MS = 120_000;

export type DesktopConfig = {
  endpoint: string;
  locale: Locale;
  defaultTimeoutMs: number;
  maxTimeoutMs: number;
  // Only for development: a Vite server on this machine to load instead of the built page.
  devServerUrl?: string;
};

// "30s", "2m", "1500ms"; a bare number is milliseconds.
export function parseDuration(value: string | undefined): number | undefined {
  const match = /^\s*(\d+(?:\.\d+)?)\s*(ms|s|m)?\s*$/i.exec(value ?? "");
  if (!match) return undefined;
  const unit = (match[2] ?? "ms").toLowerCase();
  const factor = unit === "m" ? 60_000 : unit === "s" ? 1_000 : 1;
  const milliseconds = Math.round(Number(match[1]) * factor);
  return milliseconds > 0 ? milliseconds : undefined;
}

function isLocalUrl(value: string | undefined): value is string {
  if (!value) return false;
  try {
    const url = new URL(value);
    return (
      (url.protocol === "http:" || url.protocol === "https:") &&
      ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)
    );
  } catch {
    return false;
  }
}

// Flags win over the environment, like the TUI: --request-endpoint, --lang and --timeout (the
// longest a request may wait). The locale falls back to the environment, then the system.
export function parseConfig(
  args: string[],
  env: NodeJS.ProcessEnv,
  systemLocale: string,
): DesktopConfig {
  const { values } = parseArgs({
    args,
    options: {
      "request-endpoint": { type: "string" },
      lang: { type: "string" },
      timeout: { type: "string" },
    },
    strict: false,
    allowPositionals: true,
  });
  const flag = (name: string): string | undefined => {
    const value = values[name];
    return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined;
  };
  const variable = (name: string): string | undefined => env[name]?.trim() || undefined;

  const localeTag =
    flag("lang") ??
    variable("DOKJA_LANG") ??
    variable("LC_ALL") ??
    variable("LC_MESSAGES") ??
    variable("LANG") ??
    systemLocale;

  const maxTimeoutMs = parseDuration(flag("timeout")) ?? DEFAULT_MAX_TIMEOUT_MS;
  const devServerUrl = variable("DOKJA_DESKTOP_DEV_URL");

  return {
    endpoint: flag("request-endpoint") ?? variable("DOKJA_ORCH_REQUEST_ENDPOINT") ?? DEFAULT_ENDPOINT,
    locale: localeFromTag(localeTag),
    defaultTimeoutMs: Math.min(DEFAULT_REQUEST_TIMEOUT_MS, maxTimeoutMs),
    maxTimeoutMs,
    ...(isLocalUrl(devServerUrl) ? { devServerUrl } : {}),
  };
}
