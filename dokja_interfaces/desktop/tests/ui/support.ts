import type { ActionType } from "../../src/shared/actions.js";
import type {
  KnowledgeHit,
  KnowledgeSearch,
  KnowledgeStatus,
  MemeItem,
  MemePage,
  MemeStatus,
  MemoryScore,
  MemoryStatus,
  StatusReply,
} from "../../src/shared/replies.js";
import type {
  PreviewResult,
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

type Preview = (url: string) => Promise<PreviewResult>;

// A screen test that does not look at previews gets none.
const NO_PREVIEW: Preview = async () => ({
  ok: false,
  error: { code: "denied", message: "no preview in this test" },
});

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
    preview: NO_PREVIEW,
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
export function routedTransport(
  routes: Partial<Record<ActionType, Route | Route[]>>,
  preview: Preview = NO_PREVIEW,
) {
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
    preview,
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

export const MEME_STATUS: MemeStatus = { status: "ok", unsent: 30, sent: 8 };

export const meme = (number: number, overrides: Partial<MemeItem> = {}): MemeItem => ({
  url: `https://images.example/${number}.png`,
  title: `Meme ${number}`,
  tags: "",
  source: "source-1",
  createdAt: "2026-09-03T10:00:00",
  sentAt: "",
  ...overrides,
});

export const memePage = (memes: MemeItem[], overrides: Partial<MemePage> = {}): MemePage => ({
  total: memes.length,
  offset: 0,
  memes,
  ...overrides,
});

// A preview that answers with a data URL named after the address, and records what it was asked.
export function previewer(failing: string[] = []) {
  const asked: string[] = [];
  const preview: (url: string) => Promise<PreviewResult> = async (url) => {
    asked.push(url);
    if (failing.some((part) => url.includes(part))) {
      return { ok: false, error: { code: "unavailable", message: "the image host did not answer" } };
    }
    return { ok: true, dataUrl: `data:image/png;base64,${btoa(url)}` };
  };
  return { asked, preview };
};
