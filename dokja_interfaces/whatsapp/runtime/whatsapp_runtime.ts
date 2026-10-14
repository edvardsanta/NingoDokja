import { randomBytes } from "node:crypto";

import {
  createWhatsAppMessageHandler,
  type WhatsAppMessageConfig,
  type WhatsAppMessageHandler,
} from "../messages/message_handler.js";
import type { Recorder } from "../messages/recorder.js";
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
  // Removes this device from the account, on WhatsApp's side as well.
  logout(): Promise<void>;
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
  // Erases the saved session, so the next connection asks for a new link code.
  wipeAuth: (dir: string) => Promise<void>;
  connect: (auth: AuthBundle) => WhatsAppSocketLike;
  renderQr: (qr: string) => void | Promise<void>;
  handler?: WhatsAppMessageHandler;
  // Records the conversations the owner named. Absent when none is.
  recorder?: Recorder;
  // Builds the id of a message the bot is about to send, from the account's own id.
  newMessageId?: (userJid?: string) => string;
  sleep?: (ms: number) => Promise<void>;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

// The closed list of states the interface reports. None of them carries an id of any kind.
export type InterfaceState =
  | "connecting"
  | "connected"
  | "waiting_for_link"
  | "logged_out"
  | "replaced"
  | "refused"
  | "off";

export type RuntimeStatus = { state: InterfaceState; detail: string };

export type WhatsAppRuntime = {
  // Resolves after stop(). It never gives up on its own: a session that cannot continue without
  // the operator stays in a state that says so, and the process keeps answering its status.
  run(): Promise<void>;
  stop(): void;
  status(): RuntimeStatus;
  // Leaves a waiting state at once (a new link attempt, or a retry after a refusal).
  restart(): void;
  // The code to scan, only while the state is `waiting_for_link`.
  linkCode(): string | undefined;
  // Unlinks the account (on WhatsApp's side too, when it is connected), erases the saved session and
  // asks for a new link code. Nothing else of the interface is touched.
  resetSession(): Promise<void>;
};

// The values of Baileys' DisconnectReason this loop reacts to (a test keeps them in step).
export const disconnectReason = { loggedOut: 401, connectionReplaced: 440, restartRequired: 515 } as const;

const firstRetryMs = 1000;
const maxRetryMs = 60_000;
// Between two link attempts nobody scanned: a fresh code soon, then less and less often, so an
// unlinked service left running does not keep asking WhatsApp for codes.
const firstLinkRetryMs = 5000;
const maxLinkRetryMs = 5 * 60_000;
// Failures in a row before WhatsApp ever issued a link code. A few are a network that is not
// ready yet; this many are a client WhatsApp no longer accepts.
const maxFailuresBeforeLinkCode = 5;

type Outcome = { code: number | undefined; opened: boolean; linked: boolean; linkCodeShown: boolean };

export function createWhatsAppRuntime(config: RuntimeConfig, deps: Dependencies): WhatsAppRuntime {
  const handler = deps.handler ?? createWhatsAppMessageHandler(config, { log: deps.log, error: deps.error });
  const sent = new RecentKeys();
  const sleep = deps.sleep ?? ((ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)));
  const newMessageId = deps.newMessageId ?? randomMessageId;
  let stopped = false;
  let current: WhatsAppSocketLike | null = null;
  let status: RuntimeStatus = { state: "connecting", detail: "" };
  let wake: (() => void) | null = null;
  let linkCode: string | undefined;
  let resetPending = false;

  function setState(state: InterfaceState, detail = ""): void {
    status = { state, detail };
    // A code is only worth showing while the account waits for it.
    if (state !== "waiting_for_link") {
      linkCode = undefined;
    }
    // These are the ones that need the operator; the others are already logged where they happen.
    if (state === "logged_out" || state === "replaced" || state === "refused") {
      deps.log?.(JSON.stringify({ whatsapp: state, detail }));
    }
  }

  // Waits for stop() or restart(), or for the given time when there is one.
  function waitForWake(ms?: number): Promise<void> {
    return new Promise<void>((resolve) => {
      let done = false;
      const finish = () => {
        if (done) {
          return;
        }
        done = true;
        if (wake === finish) {
          wake = null;
        }
        resolve();
      };
      wake = finish;
      if (ms !== undefined) {
        void sleep(ms).then(finish);
      }
    });
  }

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
        newMessageId: () => newMessageId(socket.user?.id),
      });
    } catch {
      deps.error?.(JSON.stringify({ message: "unreadable message" }));
      return;
    }
    if (deps.recorder) {
      const recorder = deps.recorder;
      void recorder.inbound(context);
      // What the bot sends into a named conversation is recorded with the id it went out with.
      const send = context.reply;
      context.reply = async (text) => {
        const result = await send(text);
        const id = (result as { key?: { id?: string | null } } | undefined)?.key?.id ?? undefined;
        void recorder.outbound(context, text, id);
        return result;
      };
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
      let linkCodeShown = false;
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
          linkCodeShown = true;
          setState("waiting_for_link");
          linkCode = update.qr;
          deps.log?.(JSON.stringify({ whatsapp: "waiting for the link", hint: "scan with Settings > Linked devices" }));
          void Promise.resolve(deps.renderQr(update.qr)).catch(() => {
            deps.error?.(JSON.stringify({ message: "could not show the link code" }));
          });
        }
        if (update.connection === "open") {
          opened = true;
          setState("connected");
          deps.log?.(JSON.stringify({ whatsapp: "connected" }));
        }
        if (update.connection === "close") {
          resolve({
            code: statusCodeOf(update.lastDisconnect?.error),
            opened,
            linked: Boolean(auth.state.creds.me),
            linkCodeShown,
          });
        }
      });
    });
  }

  return {
    async run() {
      let attempt = 0;
      let linkAttempt = 0;
      let failuresBeforeLinkCode = 0;
      while (!stopped) {
        if (resetPending) {
          resetPending = false;
          try {
            await deps.wipeAuth(config.authDir);
            deps.log?.(JSON.stringify({ whatsapp: "session erased", next: "a new link code follows" }));
          } catch (error) {
            deps.error?.(JSON.stringify({
              message: "could not erase the session",
              error: error instanceof Error ? error.message : String(error),
            }));
          }
          attempt = 0;
          linkAttempt = 0;
          failuresBeforeLinkCode = 0;
        }
        setState("connecting");
        let outcome: Outcome;
        try {
          outcome = await connectOnce();
        } catch (error) {
          deps.error?.(JSON.stringify({
            message: "could not connect",
            error: error instanceof Error ? error.message : String(error),
          }));
          outcome = { code: undefined, opened: false, linked: true, linkCodeShown: false };
        }
        if (stopped) {
          break;
        }
        // The operator asked to start over: this close was theirs, and the top of the loop erases
        // the session before the next connection.
        if (resetPending) {
          continue;
        }
        if (outcome.opened) {
          attempt = 0;
          linkAttempt = 0;
          failuresBeforeLinkCode = 0;
        }

        // Linking ends with the server asking for a new connection: that is the normal path.
        if (outcome.code === disconnectReason.restartRequired) {
          deps.log?.(JSON.stringify({ whatsapp: "reconnecting", reason: "restart required" }));
          continue;
        }
        if (outcome.code === disconnectReason.loggedOut) {
          setState("logged_out", "logged out from the phone; remove the saved session and link again");
          await waitForWake();
          continue;
        }
        if (outcome.code === disconnectReason.connectionReplaced) {
          setState("replaced", "another connection is using the session");
          await waitForWake();
          continue;
        }
        if (!outcome.linked) {
          if (outcome.linkCodeShown) {
            const delay = Math.min(maxLinkRetryMs, firstLinkRetryMs * 2 ** linkAttempt);
            linkAttempt += 1;
            setState("waiting_for_link", "the last link code expired; a new one follows");
            deps.log?.(JSON.stringify({
              whatsapp: "link code expired",
              status: describe(outcome.code),
              newCodeInMs: delay,
            }));
            await waitForWake(delay);
            continue;
          }
          failuresBeforeLinkCode += 1;
          if (failuresBeforeLinkCode >= maxFailuresBeforeLinkCode) {
            setState(
              "refused",
              `closed ${failuresBeforeLinkCode} times before issuing a link code (status ${describe(outcome.code)}); ` +
                "if the network is fine the Baileys version probably needs an upgrade",
            );
            failuresBeforeLinkCode = 0;
            await waitForWake();
            continue;
          }
        }

        const delay = Math.min(maxRetryMs, firstRetryMs * 2 ** attempt);
        attempt += 1;
        deps.log?.(JSON.stringify({
          whatsapp: "reconnecting",
          reason: "connection lost",
          status: describe(outcome.code),
          inMs: delay,
        }));
        await waitForWake(delay);
      }
      setState("off");
    },
    stop() {
      stopped = true;
      setState("off");
      wake?.();
      current?.end(undefined);
    },
    status() {
      return status;
    },
    restart() {
      wake?.();
    },
    linkCode() {
      return status.state === "waiting_for_link" ? linkCode : undefined;
    },
    async resetSession() {
      resetPending = true;
      const socket = current;
      if (socket && status.state === "connected") {
        // Tell WhatsApp to drop this device; if that fails the phone may still list it, and the
        // operator can remove it there.
        try {
          await socket.logout();
        } catch {
          deps.error?.(JSON.stringify({ message: "could not unlink on WhatsApp's side" }));
        }
      }
      socket?.end(undefined);
      wake?.();
    },
  };
}

// The id of a message in the shape the official client uses. Only used when none is injected.
function randomMessageId(): string {
  return `3EB0${randomBytes(9).toString("hex").toUpperCase()}`;
}

function describe(code: number | undefined): string {
  return code === undefined ? "unknown" : String(code);
}

function statusCodeOf(error: unknown): number | undefined {
  const code = (error as { output?: { statusCode?: unknown } } | undefined)?.output?.statusCode;
  return typeof code === "number" ? code : undefined;
}
