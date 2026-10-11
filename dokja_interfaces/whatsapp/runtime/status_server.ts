import { createServer, type Server, type ServerResponse } from "node:http";

import type { HistoryEntry } from "../history/store.js";
import { qrRows } from "./qr.js";
import type { InterfaceState, RuntimeStatus } from "./whatsapp_runtime.js";

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
};

// What the desktop's conversations view needs: the named conversations (only their aliases) and
// their recorded text, a page at a time.
export type ConversationControls = {
  list: () => Array<{ alias: string; kind: "direct" | "group" }>;
  retentionDays: number;
  page: (alias: string, before?: number) => Promise<{ messages: HistoryEntry[]; before: string }>;
};

type Options = {
  port: number;
  status: () => RuntimeStatus;
  link?: LinkControls;
  conversations?: ConversationControls;
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
