import type {
  KnowledgeSearch,
  KnowledgeStatus,
  MemeItem,
  MemePage,
  MemeStatus,
  MemoryScore,
  MemoryStatus,
  StatusReply,
} from "../shared/replies.js";
import type { PreviewResult, Transport, TransportResult } from "../shared/transport.js";

// Made-up data for `pnpm dev:web`, where there is no orchestrator and no shell. It exists only in
// development: the built page never includes it.

const STATUS: StatusReply = {
  status: "degraded",
  services: {
    meme: { status: "ok", detail: "" },
    chat_ai: { status: "error", detail: "connection refused" },
    book: { status: "unchecked", detail: "" },
    memory: { status: "ok", detail: "" },
    scheduler: { status: "stopped", detail: "no announce for 12m (stopped?)" },
  },
};

const MEMORY_STATUS: MemoryStatus = {
  experiences: 48,
  pending: 14,
  resolved: 31,
  expired: 3,
  embedded: 46,
  needsReindex: 2,
  embedModel: "model-1",
  embeddings: true,
  embedderReachable: true,
};

const MEMORY_SCORE: MemoryScore = {
  scored: 31,
  unscored: 4,
  minScored: 30,
  brierPrediction: 0.081,
  brierBaseline: 0.27,
  skill: 0.7,
  beatsBaseline: true,
  enoughData: true,
};

const KNOWLEDGE_STATUS: KnowledgeStatus = {
  documents: 6,
  chunks: 52,
  embedded: 50,
  pendingEmbeddings: 2,
  embedModel: "model-1",
  embedderReachable: true,
};

function searchFor(query: string): KnowledgeSearch {
  return {
    query,
    relevantCount: 2,
    threshold: 0.45,
    degraded: false,
    reason: "",
    hits: [
      {
        rank: 1,
        title: "On the freedom of choosing",
        heading: "Chapter two",
        kind: "note",
        sourceRef: "notes/freedom.md",
        tags: ["ethics"],
        text: "What we choose is shaped by what we are, and what we are was shaped before we chose.",
        score: 0.68,
        relevant: true,
      },
      {
        rank: 2,
        title: "Reading list",
        heading: "",
        kind: "list",
        sourceRef: "notes/reading.md",
        tags: [],
        text: "A short list of books to read again this year, with a line on why each one deserves it.",
        score: 0.52,
        relevant: true,
      },
      {
        rank: 3,
        title: "A cake recipe",
        heading: "Method",
        kind: "note",
        sourceRef: "notes/cake.md",
        tags: ["kitchen"],
        text: "Mix the flour and the sugar, then fold in the eggs.",
        score: 0.21,
        relevant: false,
      },
    ],
  };
}

const MEME_STATUS: MemeStatus = { status: "ok", unsent: 30, sent: 8 };

function memes(scope: "unsent" | "sent"): MemeItem[] {
  const count = scope === "sent" ? MEME_STATUS.sent : MEME_STATUS.unsent;
  return Array.from({ length: count }, (_, index) => {
    const number = index + 1;
    const kind = number % 11 === 0 ? ".mp4" : number % 7 === 0 ? "-broken.png" : ".png";
    return {
      url: `https://images.example/${scope}/${number}${kind}`,
      title: number % 5 === 0 ? "" : `Placeholder meme ${number}`,
      tags: number % 3 === 0 ? "reaction, cat" : "",
      source: `source-${(number % 3) + 1}`,
      createdAt: `2026-09-${String((number % 28) + 1).padStart(2, "0")}T10:00:00`,
      sentAt: scope === "sent" ? `2026-10-01T09:${String(number).padStart(2, "0")}:00` : "",
    };
  });
}

// A coloured card with the number of the image, so every address looks different.
function placeholder(url: string): string {
  const seed = [...url].reduce((sum, character) => sum + character.charCodeAt(0), 0);
  const label = url.split("/").slice(-2).join("/");
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="240">` +
    `<rect width="320" height="240" fill="hsl(${seed % 360} 45% 22%)"/>` +
    `<rect x="12" y="12" width="296" height="216" fill="none" stroke="hsl(${(seed + 180) % 360} 80% 60%)" stroke-width="2"/>` +
    `<text x="160" y="126" fill="#d6e6f3" font-family="monospace" font-size="18" text-anchor="middle">${label}</text>` +
    `</svg>`;
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

const ok = (result: unknown): TransportResult => ({ ok: true, result });
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export function createFakeTransport(delayMs = 400): Transport {
  return {
    async request(type, payload) {
      await sleep(delayMs);
      switch (type) {
        case "ningo.status":
          return ok(STATUS);
        case "memory.status":
          return ok(MEMORY_STATUS);
        case "memory.stats":
          return ok(MEMORY_SCORE);
        case "knowledge.status":
          return ok(KNOWLEDGE_STATUS);
        case "knowledge.search":
          return ok(searchFor(String(payload?.query ?? "")));
        case "meme.status":
          return ok(MEME_STATUS);
        case "meme.list": {
          const scope = payload?.scope === "sent" ? "sent" : "unsent";
          const limit = Number(payload?.limit) || 12;
          const offset = Number(payload?.offset) || 0;
          const all = memes(scope);
          const page: MemePage = { total: all.length, offset, memes: all.slice(offset, offset + limit) };
          return ok(page);
        }
      }
    },

    async preview(url): Promise<PreviewResult> {
      await sleep(150 + (url.length % 7) * 90);
      if (url.includes("broken")) {
        return { ok: false, error: { code: "unavailable", message: "the image host did not answer" } };
      }
      return { ok: true, dataUrl: placeholder(url) };
    },
  };
}
