import { randomUUID } from "node:crypto";

import { Request } from "zeromq";

import { isRecord } from "../shared/records.js";
import type { TransportErrorCode } from "../shared/transport.js";

export const SOURCE = "desktop";

const DEFAULT_CONNECT_TIMEOUT_MS = 3_000;

export class OrchestratorError extends Error {
  readonly code: TransportErrorCode;

  constructor(code: TransportErrorCode, message: string) {
    super(message);
    this.name = "OrchestratorError";
    this.code = code;
  }
}

export type OrchestratorReply = { status: string; result?: unknown; error?: string };

type ClientOptions = {
  endpoint: string;
  // How long to wait for the orchestrator to accept a request before calling it unavailable.
  connectTimeoutMs?: number;
  communicationsToken?: string;
};

// Sends requests to the orchestrator over ZeroMQ REQ/REP, the same wire format the CLI and the
// TUI use. The orchestrator answers one request at a time, so requests wait here in line.
export class OrchestratorClient {
  private readonly endpoint: string;
  private readonly communicationsToken: string;
  private readonly connectTimeoutMs: number;
  private line: Promise<void> = Promise.resolve();

  constructor(options: ClientOptions) {
    this.endpoint = options.endpoint;
    this.communicationsToken = options.communicationsToken ?? "";
    this.connectTimeoutMs = options.connectTimeoutMs ?? DEFAULT_CONNECT_TIMEOUT_MS;
  }

  request(
    type: string,
    payload: Record<string, unknown>,
    timeoutMs: number,
  ): Promise<OrchestratorReply> {
    if (type.startsWith("communications.")) {
      if (this.communicationsToken.length < 32 || this.communicationsToken.startsWith("CHANGE_ME")) {
        return Promise.reject(new OrchestratorError("orchestrator", "Communications access is not configured."));
      }
      if (!localEndpoint(this.endpoint)) {
        return Promise.reject(new OrchestratorError("denied", "communications requires a local endpoint or a local tunnel"));
      }
    }
    const deadline = Date.now() + timeoutMs;
    return new Promise<OrchestratorReply>((resolve, reject) => {
      let settled = false;
      const settle = (action: () => void): void => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        action();
      };
      // The deadline counts from the call, not from the turn in line: a request stuck behind a
      // slow one gives up on time and is never sent.
      const timer = setTimeout(() => {
        settle(() => reject(new OrchestratorError("timeout", `no answer within ${timeoutMs} ms`)));
      }, timeoutMs);

      this.line = this.line.then(async () => {
        if (settled) return;
        try {
          const reply = await this.exchange(type, payload, deadline);
          settle(() => resolve(reply));
        } catch (error) {
          settle(() => reject(toOrchestratorError(error)));
        }
      });
    });
  }

  private async exchange(
    type: string,
    payload: Record<string, unknown>,
    deadline: number,
  ): Promise<OrchestratorReply> {
    const remaining = deadline - Date.now();
    if (remaining <= 0) {
      throw new OrchestratorError("timeout", "the deadline passed while waiting in line");
    }

    // One socket per request. A REQ socket that timed out can never send again, and a late reply
    // must not be taken for the answer to the next request. "immediate" makes send fail fast when
    // nobody is listening, instead of queueing the request until the deadline.
    const socket = new Request({
      immediate: true,
      linger: 0,
      sendTimeout: Math.min(this.connectTimeoutMs, remaining),
    });
    try {
      socket.connect(this.endpoint);
      try {
        await socket.send(JSON.stringify(buildEnvelope(type, payload, this.communicationsToken)));
      } catch (error) {
        throw new OrchestratorError("unavailable", describe("the orchestrator did not accept the request", error));
      }

      const waiting = deadline - Date.now();
      if (waiting <= 0) {
        throw new OrchestratorError("timeout", "the deadline passed before the reply");
      }
      socket.receiveTimeout = waiting;
      let frames: Uint8Array[];
      try {
        frames = await socket.receive();
      } catch (error) {
        throw new OrchestratorError("timeout", describe("no reply", error));
      }
      return parseReply(frames[0]);
    } finally {
      socket.close();
    }
  }
}

function buildEnvelope(type: string, payload: Record<string, unknown>, token: string) {
  return {
    event_id: randomUUID(),
    timestamp: new Date().toISOString(),
    source: SOURCE,
    type,
    user: { id: SOURCE, name: SOURCE },
    channel: { id: SOURCE },
    payload,
    context: { interface: SOURCE, ...(type.startsWith("communications.") ? { communications_token: token } : {}) },
  };
}

function parseReply(frame: Uint8Array | undefined): OrchestratorReply {
  let value: unknown;
  try {
    value = JSON.parse(Buffer.from(frame ?? new Uint8Array()).toString("utf8"));
  } catch {
    throw new OrchestratorError("orchestrator", "the reply is not valid JSON");
  }
  if (!isRecord(value) || typeof value.status !== "string") {
    throw new OrchestratorError("orchestrator", "the reply has no status");
  }
  return {
    status: value.status,
    result: value.result,
    error: typeof value.error === "string" ? value.error : undefined,
  };
}

function toOrchestratorError(error: unknown): OrchestratorError {
  return error instanceof OrchestratorError
    ? error
    : new OrchestratorError("unavailable", describe("request failed", error));
}

function describe(prefix: string, error: unknown): string {
  const detail = error instanceof Error ? error.message : String(error);
  return `${prefix}: ${detail}`;
}

function localEndpoint(endpoint: string): boolean {
  if (endpoint.startsWith("ipc:///")) return true;
  try {
    const url = new URL(endpoint);
    return url.protocol === "tcp:" && ["127.0.0.1", "[::1]", "localhost"].includes(url.hostname);
  } catch { return false; }
}
