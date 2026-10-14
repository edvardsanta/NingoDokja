import { chmod, mkdir } from "node:fs/promises";

import makeWASocket, {
  Browsers,
  generateMessageIDV2,
  makeCacheableSignalKeyStore,
  useMultiFileAuthState,
} from "baileys";

import { SentMessages } from "./sent_messages.js";
import type { AuthBundle, WhatsAppSocketLike } from "./whatsapp_runtime.js";

// The only file that talks to Baileys. The session folder holds the keys of a linked device,
// so only the owner may read it.
export async function loadAuth(dir: string): Promise<AuthBundle> {
  await mkdir(dir, { recursive: true, mode: 0o700 });
  await chmod(dir, 0o700);
  return useMultiFileAuthState(dir);
}

// The part of the Baileys socket the adapter reads. Sending resolves with the message as it was
// sent, which carries the id and the content.
export type RawSocket = Omit<WhatsAppSocketLike, "sendMessage"> & {
  sendMessage(
    jid: string,
    content: { text: string },
    options?: { quoted?: unknown; messageId?: string },
  ): Promise<{ key?: { id?: string | null }; message?: unknown } | undefined>;
};

export function connectSocket(auth: AuthBundle, error: (...args: unknown[]) => void): WhatsAppSocketLike {
  const logger = quietLogger(error);
  const bundle = auth as Awaited<ReturnType<typeof useMultiFileAuthState>>;
  const sent = new SentMessages<unknown>();
  const socket = makeWASocket({
    auth: { creds: bundle.state.creds, keys: makeCacheableSignalKeyStore(bundle.state.keys, logger) },
    logger,
    browser: Browsers.ubuntu("Ningo"),
    // Online presence would silence the phone's own notifications for the whole account.
    markOnlineOnConnect: false,
    syncFullHistory: false,
    generateHighQualityLinkPreview: false,
    getMessage: async (key) => lookupSent(sent, key.id) as never,
  }) as unknown as RawSocket;
  return adaptSocket(socket, sent);
}

// What WhatsApp asks for again: only the bot's own recent messages are known.
export function lookupSent(sent: SentMessages<unknown>, id: string | null | undefined): unknown {
  return id ? sent.get(id) : undefined;
}

// Remembers the content of every message sent, so a device that could not decrypt one can get it again.
export function adaptSocket(socket: RawSocket, sent: SentMessages<unknown>): WhatsAppSocketLike {
  return {
    ev: socket.ev,
    get user() {
      return socket.user;
    },
    signalRepository: socket.signalRepository,
    sendPresenceUpdate: (type, jid) => socket.sendPresenceUpdate(type, jid),
    end: (reason) => socket.end(reason),
    async sendMessage(jid, content, options) {
      const result = await socket.sendMessage(jid, content, options);
      if (result?.key?.id && result.message) {
        sent.set(result.key.id, result.message);
      }
      return result;
    },
  };
}

// The id of a message in the format the official client uses, so the bot's messages look like any other's.
export function newMessageId(userJid?: string): string {
  return generateMessageIDV2(userJid);
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
