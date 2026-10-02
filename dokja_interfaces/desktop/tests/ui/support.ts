import type { StatusReply } from "../../src/shared/replies.js";
import type {
  RequestOptions,
  Transport,
  TransportErrorCode,
  TransportResult,
} from "../../src/shared/transport.js";

export type Call = {
  type: string;
  payload: Record<string, unknown> | undefined;
  options: RequestOptions | undefined;
};

type Scripted = TransportResult | (() => Promise<TransportResult>);

// A transport that answers from a script and records what it was asked. The last reply repeats;
// a function reply lets a test decide when an answer arrives.
export function scriptedTransport(...replies: Scripted[]) {
  const calls: Call[] = [];
  const transport: Transport = {
    async request(type, payload, options) {
      calls.push({ type, payload, options });
      const reply = replies[Math.min(calls.length - 1, replies.length - 1)];
      if (!reply) throw new Error("no scripted reply");
      return typeof reply === "function" ? reply() : reply;
    },
  };
  return { transport, calls };
}

export function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

// Hand-built replies, never captured ones.
export const SERVICES: StatusReply["services"] = {
  meme: { status: "ok", detail: "" },
  chat_ai: { status: "error", detail: "connection refused" },
  scheduler: { status: "stopped", detail: "never announced" },
  memory: { status: "disabled", detail: "", enabled: false },
  book: { status: "unchecked", detail: "" },
};

export const ALL_UP: StatusReply["services"] = {
  meme: { status: "ok", detail: "" },
  chat_ai: { status: "ok", detail: "" },
};

export function statusReply(
  services: StatusReply["services"],
  status = "ok",
): TransportResult {
  return { ok: true, result: { status, services } satisfies StatusReply };
}

export function failure(code: TransportErrorCode, message = ""): TransportResult {
  return { ok: false, error: { code, message } };
}
