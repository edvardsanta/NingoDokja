import {
  createWhatsAppMessageHandler,
  type WhatsAppMessageConfig,
  type WhatsAppMessageHandler,
} from "../messages/message_handler.js";
import { RecentKeys } from "../messages/recent_keys.js";
import { toMessageContext, type InboundMessage } from "./inbound.js";

// The socket events this adapter listens to.
export type SocketEvents = {
  "connection.update": { connection?: string; qr?: string; lastDisconnect?: { error?: unknown } };
  "creds.update": unknown;
  "messages.upsert": { type: string; messages: InboundMessage[] };
};

// The part of the Baileys socket this adapter uses, so tests can stand in for it.
export type WhatsAppSocketLike = {
  ev: { on<E extends keyof SocketEvents>(event: E, listener: (update: SocketEvents[E]) => void): unknown };
  user?: { id?: string; lid?: string };
  sendMessage(
    jid: string,
    content: { text: string },
    options?: { quoted?: unknown; messageId?: string },
  ): Promise<unknown>;
  sendPresenceUpdate(type: "composing" | "paused", jid?: string): Promise<void>;
  signalRepository?: { lidMapping?: { getPNForLID(lid: string): Promise<string | null> } };
  end(error: Error | undefined): void;
};

export type AuthBundle = {
  // `me` is set once the account is linked.
  state: { creds: { me?: unknown } };
  saveCreds: () => Promise<void>;
};

export type RuntimeConfig = WhatsAppMessageConfig & { authDir: string };

type Dependencies = {
  loadAuth: (dir: string) => Promise<AuthBundle>;
  connect: (auth: AuthBundle) => WhatsAppSocketLike;
  renderQr: (qr: string) => void | Promise<void>;
  handler?: WhatsAppMessageHandler;
  newMessageId?: () => string;
  sleep?: (ms: number) => Promise<void>;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

export type WhatsAppRuntime = {
  // Resolves after stop(); rejects when the session cannot continue without the operator.
  run(): Promise<void>;
  stop(): void;
};

// The values of Baileys' DisconnectReason this loop reacts to (a test keeps them in step).
export const disconnectReason = { loggedOut: 401, connectionReplaced: 440, restartRequired: 515 } as const;

const firstRetryMs = 1000;
const maxRetryMs = 60_000;

type Outcome = { code: number | undefined; opened: boolean; linked: boolean };

export function createWhatsAppRuntime(config: RuntimeConfig, deps: Dependencies): WhatsAppRuntime {
  const handler = deps.handler ?? createWhatsAppMessageHandler(config, { log: deps.log, error: deps.error });
  const sent = new RecentKeys();
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
  const newMessageId = deps.newMessageId ?? (() => `DOKJA${crypto.randomUUID().replaceAll("-", "").toUpperCase()}`);
  let stopped = false;
  let current: WhatsAppSocketLike | null = null;

  async function processMessage(socket: WhatsAppSocketLike, message: InboundMessage): Promise<void> {
    let context;
    try {
      context = await toMessageContext(message, {
        ownJids: () => [socket.user?.id, socket.user?.lid].filter((jid): jid is string => !!jid),
        sent,
        send: (chatId, text, options) => socket.sendMessage(chatId, { text }, options),
        presence: (chatId, active) => socket.sendPresenceUpdate(active ? "composing" : "paused", chatId),
        resolveNumber: socket.signalRepository?.lidMapping
          ? (jid) => socket.signalRepository!.lidMapping!.getPNForLID(jid)
          : undefined,
        newMessageId,
      });
    } catch {
      deps.error?.(JSON.stringify({ message: "unreadable message" }));
      return;
    }
    try {
      await handler(context);
    } catch (error) {
      deps.error?.(JSON.stringify({
        message: "handler failed",
        messageId: context.messageId,
        error: error instanceof Error ? error.message : String(error),
      }));
    }
  }

  async function connectOnce(): Promise<Outcome> {
    const auth = await deps.loadAuth(config.authDir);
    const socket = deps.connect(auth);
    current = socket;

    return new Promise<Outcome>((resolve) => {
      let opened = false;
      let saving: Promise<unknown> = Promise.resolve();

      socket.ev.on("creds.update", () => {
        // Saved one after the other: two writes of the same file must not interleave.
        saving = saving
          .then(() => auth.saveCreds())
          .catch((error: unknown) => {
            deps.error?.(JSON.stringify({
              message: "could not save the session",
              error: error instanceof Error ? error.message : String(error),
            }));
          });
      });

      socket.ev.on("messages.upsert", (upsert) => {
        // "append" is history the account already had, not something sent to the bot now.
        if (upsert.type !== "notify") {
          return;
        }
        for (const message of upsert.messages) {
          void processMessage(socket, message);
        }
      });

      socket.ev.on("connection.update", (update) => {
        if (update.qr) {
          deps.log?.(JSON.stringify({ whatsapp: "waiting for the link", hint: "scan with Settings > Linked devices" }));
          void Promise.resolve(deps.renderQr(update.qr)).catch(() => {
            deps.error?.(JSON.stringify({ message: "could not show the link code" }));
          });
        }
        if (update.connection === "open") {
          opened = true;
          deps.log?.(JSON.stringify({ whatsapp: "connected" }));
        }
        if (update.connection === "close") {
          resolve({
            code: statusCodeOf(update.lastDisconnect?.error),
            opened,
            linked: Boolean(auth.state.creds.me),
          });
        }
      });
    });
  }

  return {
    async run() {
      let attempt = 0;
      while (!stopped) {
        let outcome: Outcome;
        try {
          outcome = await connectOnce();
        } catch (error) {
          deps.error?.(JSON.stringify({
            message: "could not connect",
            error: error instanceof Error ? error.message : String(error),
          }));
          outcome = { code: undefined, opened: false, linked: true };
        }
        if (stopped) {
          return;
        }
        if (outcome.opened) {
          attempt = 0;
        }

        // Linking ends with the server asking for a new connection: that is the normal path.
        if (outcome.code === disconnectReason.restartRequired) {
          deps.log?.(JSON.stringify({ whatsapp: "reconnecting", reason: "restart required" }));
          continue;
        }
        if (outcome.code === disconnectReason.loggedOut) {
          throw new Error(
            "The linked session was logged out from the phone. Remove the saved session and link again.",
          );
        }
        if (outcome.code === disconnectReason.connectionReplaced) {
          throw new Error("Another connection is using this session. Stop it, or remove the saved session and link again.");
        }
        if (!outcome.linked) {
          throw new Error("The account was not linked in time. Start the service again to get a new code.");
        }

        const delay = Math.min(maxRetryMs, firstRetryMs * 2 ** attempt);
        attempt += 1;
        deps.log?.(JSON.stringify({ whatsapp: "reconnecting", reason: "connection lost", inMs: delay }));
        await sleep(delay);
      }
    },
    stop() {
      stopped = true;
      current?.end(undefined);
    },
  };
}

function statusCodeOf(error: unknown): number | undefined {
  const code = (error as { output?: { statusCode?: unknown } } | undefined)?.output?.statusCode;
  return typeof code === "number" ? code : undefined;
}
