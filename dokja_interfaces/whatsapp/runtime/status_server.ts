import { createServer, type Server } from "node:http";

import type { InterfaceState, RuntimeStatus } from "./whatsapp_runtime.js";

export type StatusReply = { status: "ok" | "degraded" | "error"; state: InterfaceState; detail: string };

// What the orchestrator reads to show this interface next to the services. Only the state and
// a fixed sentence about it: never an id, a phone number or a link code.
export function statusReply(status: RuntimeStatus): StatusReply {
  const level = status.state === "connected" ? "ok" : status.state === "connecting" ? "degraded" : "error";
  return { status: level, state: status.state, detail: status.detail };
}

type Options = {
  port: number;
  status: () => RuntimeStatus;
  log?: (...args: unknown[]) => void;
};

// Always on, whether or not anything is linked, so a session that needs the operator can say so.
// It is internal: Compose does not publish its port.
export function startStatusServer(options: Options): Server {
  const server = createServer((request, response) => {
    if (request.method === "GET" && request.url === "/status") {
      return reply(response, 200, statusReply(options.status()));
    }
    if (request.method === "GET" && request.url === "/health") {
      return reply(response, 200, { status: "ok" });
    }
    return reply(response, 404, { status: "error", message: "not found" });
  });
  server.listen(options.port, "0.0.0.0", () => {
    options.log?.(JSON.stringify({ status: "listening", port: options.port }));
  });
  return server;
}

function reply(response: import("node:http").ServerResponse, status: number, body: unknown): void {
  const payload = JSON.stringify(body);
  response.writeHead(status, { "content-type": "application/json", "content-length": Buffer.byteLength(payload) });
  response.end(payload);
}
