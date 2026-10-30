import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";

import type { HistoryEntry } from "../history/store.js";
import type { DeliverOutcome } from "../messages/desk_sender.js";
import { qrRows } from "./qr.js";
import type { InterfaceState, RuntimeStatus, WhatsAppGroup } from "./whatsapp_runtime.js";

export type StatusReply = { status: "ok" | "degraded" | "error"; state: InterfaceState; detail: string };

// What the orchestrator reads to show this interface next to the services. Only the state and
// a fixed sentence about it: never an id, a phone number or a link code.
export function statusReply(status: RuntimeStatus): StatusReply {
  const level = status.state === "connected" ? "ok" : status.state === "connecting" ? "degraded" : "error";
  return { status: level, state: status.state, detail: status.detail };
}

// What the screen that links the account needs. The code leaves the process only as the rows of the
// picture, and only while the account waits for it.
export type LinkControls = {
  code: () => string | undefined;
  // Asks for a new link attempt now.
  start: () => void;
  // Unlinks, erases the saved session and asks for a new code.
  reset: () => Promise<void>;
  // The groups the linked account is in; refuses while it is not connected.
  groups: () => Promise<WhatsAppGroup[]>;
};

// What the desktop's conversations view needs: the named conversations (only their aliases) and
// their recorded text, a page at a time.
export type ConversationControls = {
  list: () => Array<{ alias: string; kind: "direct" | "group" }>;
  retentionDays: number;
  page: (alias: string, before?: number) => Promise<{ messages: HistoryEntry[]; before: string }>;
};

// Sends what the owner typed in the desktop into a named conversation, by its alias.
export type DeliverControls = (alias: string, text: string) => Promise<DeliverOutcome>;

type Options = {
  port: number;
  status: () => RuntimeStatus;
  link?: LinkControls;
  conversations?: ConversationControls;
  deliver?: DeliverControls;
  log?: (...args: unknown[]) => void;
};

// Always on, whether or not anything is linked, so a session that needs the operator can say so.
// It is internal: Compose does not publish its port, and the orchestrator puts the credential of the
// communications actions in front of the link routes.
export function startStatusServer(options: Options): Server {
  const server = createServer(async (request, response) => {
    if (request.method === "GET" && request.url === "/status") {
      return reply(response, 200, statusReply(options.status()));
    }
    if (request.method === "GET" && request.url === "/health") {
      return reply(response, 200, { status: "ok" });
    }
    if (options.deliver && request.method === "POST" && request.url === "/deliver") {
      return deliver(request, response, options.deliver);
    }
    if (options.conversations && request.method === "GET") {
      const url = new URL(request.url ?? "/", "http://internal");
      if (url.pathname === "/conversations") {
        return reply(response, 200, {
          conversations: options.conversations.list().map((conversation) => ({
            ...conversation,
            retention_days: options.conversations!.retentionDays,
          })),
        });
      }
      if (url.pathname === "/history") {
        return history(response, url, options.conversations);
      }
    }
    if (options.link) {
      if (request.method === "GET" && request.url === "/link") {
        return reply(response, 200, linkReply(options.status(), options.link.code()));
      }
      if (request.method === "GET" && request.url === "/link/groups") {
        try {
          return reply(response, 200, { state: options.status().state, groups: await options.link.groups() });
        } catch {
          return reply(response, 409, { status: "error", message: "the account is not connected" });
        }
      }
      if (request.method === "POST" && request.url === "/link/start") {
        options.link.start();
        return reply(response, 200, linkReply(options.status(), undefined));
      }
      if (request.method === "POST" && request.url === "/link/reset") {
        try {
          await options.link.reset();
        } catch {
          return reply(response, 500, { status: "error", message: "could not reset the session" });
        }
        return reply(response, 200, linkReply(options.status(), undefined));
      }
    }
    return reply(response, 404, { status: "error", message: "not found" });
  });
  server.listen(options.port, "0.0.0.0", () => {
    options.log?.(JSON.stringify({ status: "listening", port: options.port }));
  });
  return server;
}

const maxDeliverBody = 8 * 1024;
const maxDeliverChars = 2000;
const deliverStatus: Record<DeliverOutcome, [number, string]> = {
  sent: [200, "ok"],
  not_named: [404, "not found"],
  not_connected: [409, "the account is not connected"],
  limited: [429, "too many messages; wait a moment"],
  failed: [502, "the message could not be sent"],
};

// POST /deliver {alias, content}. Only text, only into a conversation the owner named, and under a
// fixed budget. What went wrong is a fixed sentence: nothing of the chat is echoed.
async function deliver(request: IncomingMessage, response: ServerResponse, send: DeliverControls): Promise<void> {
  let body: unknown;
  try {
    body = JSON.parse(await readBody(request));
  } catch (error) {
    if (error instanceof TooLarge) {
      return reply(response, 413, { status: "error", message: "the body is too large" });
    }
    return reply(response, 400, { status: "error", message: "the body must be a small JSON object" });
  }
  const record = (typeof body === "object" && body !== null ? body : {}) as Record<string, unknown>;
  const alias = typeof record.alias === "string" ? record.alias : "";
  const content = typeof record.content === "string" ? record.content.trim() : "";
  if (!/^[a-z0-9][a-z0-9_-]{0,31}$/.test(alias) || !content || content.length > maxDeliverChars || content.includes("\u0000")) {
    return reply(response, 400, { status: "error", message: "alias and text (up to 2000 characters) are required" });
  }
  const outcome = await send(alias, content);
  const [code, message] = deliverStatus[outcome];
  return reply(response, code, outcome === "sent" ? { status: "ok", alias } : { status: "error", message });
}

class TooLarge extends Error {}

// Reads the body up to a limit. What is past it is drained and dropped, so the caller can still answer.
function readBody(request: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let size = 0;
    let tooLarge = false;
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => {
      size += chunk.length;
      if (size > maxDeliverBody) {
        tooLarge = true;
        return;
      }
      chunks.push(chunk);
    });
    request.on("end", () => (tooLarge ? reject(new TooLarge()) : resolve(Buffer.concat(chunks).toString("utf8"))));
    request.on("error", reject);
  });
}

// A page of the recorded text of one named conversation, shaped like a page of Discord messages so the
// screen that shows them is the same. An alias that is not named, or a cursor that is not a number, is
// refused before anything is read.
async function history(response: ServerResponse, url: URL, controls: ConversationControls): Promise<void> {
  const alias = url.searchParams.get("alias") ?? "";
  const before = url.searchParams.get("before") ?? "";
  if (!controls.list().some((conversation) => conversation.alias === alias)) {
    return reply(response, 404, { status: "error", message: "not found" });
  }
  if (before !== "" && !/^[0-9]{1,15}$/.test(before)) {
    return reply(response, 400, { status: "error", message: "before must be a cursor" });
  }
  const page = await controls.page(alias, before === "" ? undefined : Number(before));
  return reply(response, 200, {
    alias,
    messages: page.messages.map((entry) => ({
      id: String(entry.seq),
      author: entry.who,
      bot: entry.who === "bot",
      content: entry.text,
      timestamp: entry.at,
      edited: false,
      reply_to: "",
      attachments: [],
    })),
    before: page.before,
  });
}

// The state and, while the account waits for a link, the code as rows of "0" and "1".
function linkReply(status: RuntimeStatus, code: string | undefined) {
  return {
    state: status.state,
    ...(status.state === "waiting_for_link" && code ? { matrix: qrRows(code) } : {}),
  };
}

function reply(response: ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  response.writeHead(status, { "content-type": "application/json", "content-length": Buffer.byteLength(payload) });
  response.end(payload);
}
