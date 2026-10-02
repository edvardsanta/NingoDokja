import type { ActionType } from "../../src/shared/actions.js";
import type {
  KnowledgeHit,
  KnowledgeSearch,
  KnowledgeStatus,
  MemoryScore,
  MemoryStatus,
  StatusReply,
} from "../../src/shared/replies.js";
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

type Route = TransportResult | (() => Promise<TransportResult>);

// A transport that answers by action: each action has a reply, or a list of replies used in turn
// (the last one repeats). An action without a route is refused. Records what it was asked.
export function routedTransport(routes: Partial<Record<ActionType, Route | Route[]>>) {
  const calls: Call[] = [];
  const used = new Map<string, number>();
  const transport: Transport = {
    async request(type, payload, options) {
      calls.push({ type, payload, options });
      const route = routes[type];
      const list = Array.isArray(route) ? route : route ? [route] : [];
      const turn = used.get(type) ?? 0;
      used.set(type, turn + 1);
      const reply = list[Math.min(turn, list.length - 1)];
      if (!reply) return failure("denied", `no route for ${type}`);
      return typeof reply === "function" ? reply() : reply;
    },
  };
  return { transport, calls };
}

export const ok = (result: unknown): TransportResult => ({ ok: true, result });

// The operator switched the service off.
export const OFF = { off: true, reason: "paused by the operator" };

export const MEMORY_STATUS: MemoryStatus = {
  experiences: 40,
  pending: 12,
  resolved: 25,
  expired: 3,
  embedded: 38,
  needsReindex: 0,
  embedModel: "model-1",
  embeddings: true,
  embedderReachable: true,
};

export const MEMORY_SCORE: MemoryScore = {
  scored: 31,
  unscored: 4,
  minScored: 30,
  brierPrediction: 0.081,
  brierBaseline: 0.27,
  skill: 0.7,
  beatsBaseline: true,
  enoughData: true,
};

export const KNOWLEDGE_STATUS: KnowledgeStatus = {
  documents: 6,
  chunks: 52,
  embedded: 52,
  pendingEmbeddings: 0,
  embedModel: "model-1",
  embedderReachable: true,
};

export const hit = (overrides: Partial<KnowledgeHit> = {}): KnowledgeHit => ({
  rank: 1,
  title: "Document one",
  heading: "A section",
  kind: "note",
  sourceRef: "ref-1",
  tags: ["a", "b"],
  text: "A passage about the question.",
  score: 0.61,
  relevant: true,
  ...overrides,
});

export const search = (hits: KnowledgeHit[], overrides: Partial<KnowledgeSearch> = {}): KnowledgeSearch => ({
  query: "free will",
  hits,
  relevantCount: hits.filter((candidate) => candidate.relevant).length,
  threshold: 0.45,
  degraded: false,
  reason: "",
  ...overrides,
});
