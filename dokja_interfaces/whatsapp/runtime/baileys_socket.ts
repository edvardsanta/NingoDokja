import { chmod, mkdir } from "node:fs/promises";

import makeWASocket, { Browsers, makeCacheableSignalKeyStore, useMultiFileAuthState } from "baileys";

import type { AuthBundle, WhatsAppSocketLike } from "./whatsapp_runtime.js";

// The only file that talks to Baileys. The session folder holds the keys of a linked device,
// so only the owner may read it.
export async function loadAuth(dir: string): Promise<AuthBundle> {
  await mkdir(dir, { recursive: true, mode: 0o700 });
  await chmod(dir, 0o700);
  return useMultiFileAuthState(dir);
}

export function connectSocket(auth: AuthBundle, error: (...args: unknown[]) => void): WhatsAppSocketLike {
  const logger = quietLogger(error);
  const bundle = auth as Awaited<ReturnType<typeof useMultiFileAuthState>>;
  return makeWASocket({
    auth: { creds: bundle.state.creds, keys: makeCacheableSignalKeyStore(bundle.state.keys, logger) },
    logger,
    browser: Browsers.ubuntu("Ningo"),
    // Online presence would silence the phone's own notifications for the whole account.
    markOnlineOnConnect: false,
    syncFullHistory: false,
    generateHighQualityLinkPreview: false,
    // Nothing is kept to resend, so a retry request is simply not answered.
    getMessage: async () => undefined,
  }) as unknown as WhatsAppSocketLike;
}

type Logger = Parameters<typeof makeCacheableSignalKeyStore>[1];

// Baileys logs ids, keys and message details. Only warnings and errors are kept, and only as
// a short text: the objects it attaches can hold phone numbers or key material.
export function quietLogger(error: (...args: unknown[]) => void): NonNullable<Logger> {
  const report = (level: string, details: unknown, message?: string) => {
    const reason = (details as { err?: { message?: unknown } } | undefined)?.err?.message;
    const text = [message, typeof reason === "string" ? reason : undefined].filter(Boolean).join(": ");
    error(JSON.stringify({ message: "baileys", level, text: text.slice(0, 200) }));
  };
  const logger: NonNullable<Logger> = {
    level: "warn",
    child: () => logger,
    trace: () => undefined,
    debug: () => undefined,
    info: () => undefined,
    warn: (details, message) => report("warn", details, message),
    error: (details, message) => report("error", details, message),
  };
  return logger;
}
