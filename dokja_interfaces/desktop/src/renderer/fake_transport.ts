import type { PostReply, PostView } from "../shared/posts.js";
import { RUNTIME_MODULES } from "../shared/runtime.js";
import type {
  DeleteResult,
  DigestItem,
  IngestResult,
  KnowledgeDocument,
  KnowledgeClassification,
  KnowledgeDocuments,
  ReindexResult,
  DigestPage,
  DigestStatus,
  KnowledgeSearch,
  KnowledgeStatus,
  MemeItem,
  MemePage,
  MemeStatus,
  MemoryScore,
  MemoryStatus,
  MemoryExperience,
  StatusReply,
} from "../shared/replies.js";
import type { ChatPreview, PreviewResult, Transport, TransportResult } from "../shared/transport.js";
import type { ArchivedTurn } from "../shared/replies.js";

// An archive id as the orchestrator makes them: 32 hexadecimal digits.
const archiveId = (n: number) => n.toString(16).padStart(32, "0");

// Made-up data for `pnpm dev:web`, where there is no orchestrator and no shell. It exists only in
// development: the built page never includes it.

const STATUS: StatusReply = {
  status: "degraded",
  services: {
    meme: { status: "ok", detail: "", enabled: true },
    chat: { status: "error", detail: "connection refused", enabled: true },
    book: { status: "unchecked", detail: "", enabled: true },
    memory: { status: "ok", detail: "", enabled: true },
    scheduler: { status: "stopped", detail: "no announce for 12m (stopped?)", enabled: true },
    discord: { status: "ok", detail: "", enabled: true, kind: "interface", state: "connected" },
    whatsapp: { status: "error", detail: "", enabled: true, kind: "interface", state: "waiting_for_link" },
  },
  jobs: [
    { name: "meme.refresh", enabled: true, interval: "6h0m0s", intervalOverride: false, nextAt: "", lastAt: "", lastOutcome: "ran", lastError: "" },
    { name: "meme.dispatch", enabled: true, interval: "1h0m0s", intervalOverride: false, nextAt: "", lastAt: "", lastOutcome: "skipped", lastError: "" },
  ],
};

const MEMORY_STATUS: MemoryStatus = {
  experiences: 7,
  pending: 7,
  resolved: 0,
  expired: 0,
  embedded: 7,
  needsReindex: 0,
  embedModel: "model-1",
  embeddings: true,
  embedderReachable: true,
};

const MEMORY_SCORE: MemoryScore = {
  scored: 0,
  unscored: 0,
  minScored: 30,
  brierPrediction: 0.081,
  brierBaseline: 0.27,
  skill: 0.7,
  beatsBaseline: true,
  enoughData: false,
};

const EXPERIENCES: MemoryExperience[] = Array.from({ length: 7 }, (_, i) => ({
  ref: `fixture:experience-${i + 1}`, action: "hashtag.suggest", detail: "#Reading", outcome: "",
  predictedP: i === 0 ? null : 0.6, baselineP: i === 0 ? null : 0.5, matchedScore: 0.8,
  createdAt: "2026-10-21T10:00:00Z", resolvedAt: "", contextSnippet: `Example ${i + 1}: a joke about reading.`, matchedSnippet: "An earlier reading joke.",
  context: `Example ${i + 1}: a joke about reading.\nThis is hand-built demo content.`, contextTruncated: false,
  feedback: { code: "hashtag_tag", message: "" },
}));

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
        sourceId: "note:example-1",
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
        sourceId: "article:example-2",
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
        sourceId: "book:example-3",
        tags: ["kitchen"],
        text: "Mix the flour and the sugar, then fold in the eggs.",
        score: 0.21,
        relevant: false,
      },
    ],
  };
}

// What a document's tab shows in the browser preview.
const FAKE_TEXT = `What we call reason is a habit that learned to speak well of itself.

The second passage, stored apart from the first, joined back here as the base rebuilds it.`;

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

const DIGEST_STATUS: DigestStatus = {
  configured: true,
  directoryError: "",
  ok: 2,
  failed: 1,
  pending: 0,
  disabled: 1,
  invalid: 1,
  items: 24,
  sources: [
    { id: "local-file", name: "Local file example", state: "ok", running: false, items: 14, skipped: 0, lastOk: minutesAgo(6), error: "" },
    { id: "second-source", name: "Second example source", state: "ok", running: false, items: 10, skipped: 1, lastOk: minutesAgo(21), error: "" },
    { id: "flaky-one", name: "A flaky source", state: "failed", running: false, items: 0, skipped: 0, lastOk: "", error: "timed out after 30s" },
    { id: "idle-one", name: "idle-one", state: "disabled", running: false, items: 0, skipped: 0, lastOk: "", error: "" },
    { id: "broken-one", name: "", state: "invalid", running: false, items: 0, skipped: 0, lastOk: "", error: "program \"example-tool\" is not installed where the service runs" },
  ],
};

const SUMMARY = "Two sentences that stand in for the summary a real source would give. They are shown as plain text, never as markup, and cut after two lines.";

function digestItems(): DigestItem[] {
  return Array.from({ length: 24 }, (_, index) => {
    const number = index + 1;
    return {
      id: `item-${number}`,
      title:
        number === 3
          ? "<b>Not bold</b> & <script>nothing runs</script>"
          : `An example entry, number ${number}`,
      summary: number % 4 === 0 ? "" : SUMMARY,
      source: number % 3 === 0 ? "Second example source" : "Local file example",
      published: number === 7 ? "" : minutesAgo(number * 47),
    };
  });
}

function minutesAgo(minutes: number): string {
  return new Date(Date.now() - minutes * 60_000).toISOString();
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

// A post link in the preview. The path says what happens, so every state can be seen: /login,
// /private, /page, /broken and /reader are refused, /text has no files, anything else is a post with
// two pictures. A real post has its files behind ningo-post addresses; these are drawn on the spot.
function fakePost(url: string): PostReply {
  const path = new URL(url).pathname;
  const refusals = { login: "login_required", private: "unavailable", page: "unsupported", broken: "failed", reader: "no_reader" } as const;
  for (const [word, refused] of Object.entries(refusals)) {
    if (path.includes(word)) return { status: "refused", refused: refused as (typeof refusals)[keyof typeof refusals] };
  }
  const digits = Array.from({ length: 16 }, (_, index) => ([...url].reduce((sum, character, at) => sum + character.charCodeAt(0) * (index + at + 1), 0) % 16).toString(16));
  const post: PostView = {
    documentId: `post-${digits.join("")}`,
    url,
    author: "Example Author",
    caption: path.includes("text") ? "A thought with no picture to go with it." : "What a post says is short, and is kept with where it came from.\n\nThe pictures come after it.",
    published: "2026-10-20T12:00:00Z",
    media: path.includes("text") ? [] : [1, 2].map((number) => ({ kind: "image" as const, src: placeholder(`${url}/${number}`) })),
    missing: 0,
  };
  return { status: "ok", post };
}

function knowledgeDocuments(): KnowledgeDocument[] {
  const kinds = ["note", "article", "book", "paper", "document"];
  return Array.from({ length: 23 }, (_, index) => {
    const number = index + 1;
    return {
      id: `${kinds[index % kinds.length]}:example-${number}`,
      title: number === 4 ? "<b>Not bold</b> & <script>nothing runs</script>" : `An example document, number ${number}`,
      kind: kinds[index % kinds.length] ?? "note",
      reference: number % 3 === 0 ? "A book, page 12" : "",
      tags: number % 2 === 0 ? ["example", "demo"] : [],
      chunks: (number % 5) + 1,
      embedded: number % 7 === 0 ? 0 : (number % 5) + 1,
      updatedAt: minutesAgo(number * 130),
    };
  });
}

// A checkerboard standing in for the picture of a link code, so the panel can be seen without a phone.
const FAKE_CODE =
  "data:image/svg+xml;base64," +
  btoa(
    '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8" shape-rendering="crispEdges"><rect width="8" height="8" fill="#fff"/>' +
      '<path d="M0 0h1v1h-1zM2 0h1v1h-1zM4 0h1v1h-1zM6 0h1v1h-1zM1 1h1v1h-1zM3 1h1v1h-1zM5 1h1v1h-1zM7 1h1v1h-1z" fill="#000"/></svg>',
  );

const ok = (result: unknown): TransportResult => ({ ok: true, result });
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export function createFakeTransport(delayMs = 400): Transport {
  const experiences = structuredClone(EXPERIENCES);
  // What was added, removed and indexed stays for the life of the page, so the list follows.
  const modules=RUNTIME_MODULES.map(name=>({name,state:"stopped",error:"",dependencies:[]}));
  // Synthetic settings with placeholder ids; the demo keeps what is saved for the life of the page.
  const settings=[
    {id:"discord.meme_channels",group:"discord",kind:"list",options:[],value:"101,102",affects:["orchestrator","communications"],note:"sessions",default:""},
    {id:"discord.safe_only_channels",group:"discord",kind:"list",options:[],value:"102",affects:["orchestrator"],note:"",default:""},
    {id:"discord.answer_channels",group:"discord",kind:"list",options:[],value:"",affects:["discord"],note:"",default:""},
    {id:"discord.direct_messages",group:"discord",kind:"enum",options:["open","closed"],value:"open",affects:["discord"],note:"",default:""},
    {id:"whatsapp.people",group:"whatsapp",kind:"list",options:[],value:"",affects:["whatsapp"],note:"",default:""},
    {id:"whatsapp.groups",group:"whatsapp",kind:"list",options:[],value:"",affects:["whatsapp"],note:"",default:""},
    {id:"whatsapp.own_chat",group:"whatsapp",kind:"flag",options:["true","false"],value:"true",affects:["whatsapp"],note:"",default:""},
    {id:"models.embedding",group:"models",kind:"text",options:[],value:"",affects:["embedding_init","memory","knowledge"],note:"reindex",default:""},
  ];
  let documents = knowledgeDocuments();
  // The posts opened so far: saving one names it by id, as the shell's record of it does.
  const posts = new Map<string, PostView>();
  const classifications = new Map<string, KnowledgeClassification>();
  // Switches stay where they were put, as the orchestrator keeps them.
  let status = structuredClone(STATUS);
  const previewListeners = new Set<(preview: ChatPreview) => void>();

  // The demo's local engine: placeholder model names, and a download that is only pretend.
  const local: { models: string[]; sizes: Record<string, string>; chat: { configurationRevision: number; provider: string; model: string }; pull?: { model: string; state: string; phase?: string; percent?: number } } = {
    models: ["demo/embed:latest", "demo/small:latest"],
    sizes: { "demo/embed:latest": "1.2 GB", "demo/small:latest": "2.0 GB" },
    chat: { configurationRevision: 1, provider: "", model: "" },
  };
  // The demo's conversation archive, kept as the orchestrator keeps it: by its own opaque ids.
  const archive = new Map<string, { readingId: string; turns: ArchivedTurn[] }>();
  let conversation = [{ id: "100", author: "A reader", bot: false, content: "What have you been reading?", timestamp: new Date().toISOString(), edited: false, replyTo: "", attachments: [] as string[] }];
  return {
    onChatPreview(listener) {
      previewListeners.add(listener);
      return () => {
        previewListeners.delete(listener);
      };
    },
    async request(type, payload) {
      await sleep(delayMs);
      switch (type) {
        case "conversation.start": {
          const id = archiveId(archive.size + 1);
          archive.set(id, { readingId: String(payload?.reading_id ?? ""), turns: [] });
          return ok({ conversation: { id, kind: payload?.reading_id ? "reading" : "general", readingId: String(payload?.reading_id ?? ""), title: "", updatedAt: new Date().toISOString(), revision: 1, turns: 0 } });
        }
        case "conversation.read": {
          const held = archive.get(String(payload?.conversation_id ?? ""));
          if (!held) return { ok: false, error: { code: "orchestrator", message: "conversation not found" } };
          return ok({ conversation: { id: String(payload?.conversation_id), kind: held.readingId ? "reading" : "general", readingId: held.readingId, title: "", updatedAt: new Date().toISOString(), revision: 1, turns: held.turns.length }, turns: structuredClone(held.turns) });
        }
        case "message.created": {
          // An archived send: the same send id is answered once, and the reply arrives a few words at a
          // time, as the shell's stream would hand it over.
          const conversationId = String(payload?.conversation_id ?? "");
          const held = archive.get(conversationId);
          if (!held) return { ok: false, error: { code: "orchestrator", message: "conversation not found" } };
          const retried = held.turns.find((turn) => turn.id === payload?.retry_turn_id);
          const repeated = held.turns.find((turn) => turn.sendId === payload?.send_id);
          if (repeated) return ok({ reply: repeated.reply, replayed: true, turn: structuredClone(repeated) });
          const turn = retried ?? { id: archiveId(1000 + held.turns.length), seq: held.turns.length + 1, sendId: String(payload?.send_id ?? ""), content: String(payload?.content ?? ""), reply: "", state: "pending" as const, reason: "", attempts: 0 };
          if (!retried) held.turns.push(turn);
          turn.attempts += 1;
          const reply = "Let us look at that together, since you insist on asking.";
          const words = reply.split(" ");
          for (let count = 1; count < words.length; count++) {
            await sleep(120);
            for (const listener of previewListeners) listener({ conversationId, text: words.slice(0, count).join(" ") });
          }
          Object.assign(turn, { reply, state: "completed" });
          return ok({ reply, replayed: false, turn: structuredClone(turn) });
        }
        case "runtime.logs": return ok({module:payload?.module,text:"[demo] Module output appears here."});
        case "runtime.modules":return ok({modules:structuredClone(modules),busy:false,error:""});
        case "runtime.set": {
          const module=modules.find(m=>m.name===payload?.module);
          if(module)module.state=payload?.enabled?"ready":"stopped";
          return ok({modules:structuredClone(modules),busy:false,error:""});
        }
        case "runtime.settings": return ok({settings:structuredClone(settings),webhookChannels:["102"],busy:false,error:""});
        case "runtime.providers": return ok({ configurationRevision: 1, checkedAt: new Date().toISOString(), providers: [
          { id: "ollama", name: "Ollama", enabled: true, declaredModels: [], state: "reachable", reason: "", models: [...local.models], modelSizes: { ...local.sizes }, embeddingModel: "demo/embed:latest", modelDiscovery: "listed", inferenceStatus: "not_tested", truncated: false },
          { id: "codex", name: "Codex", enabled: true, declaredModels: [], state: "installed", reason: "", models: [], modelDiscovery: "unsupported", inferenceStatus: "not_tested", truncated: false },
        ] });
        case "runtime.chat": return ok({ ...local.chat });
        case "runtime.chat.configure":
          local.chat = { configurationRevision: local.chat.configurationRevision + 1, provider: String(payload?.provider ?? ""), model: String(payload?.model ?? "") };
          return ok({ ...local.chat });
        case "runtime.providers.configure": return ok({ provider: payload?.provider, configurationRevision: Number(payload?.configuration_revision ?? 0) + 1 });
        // A download advances a step at each read of the status, as if the engine were busy.
        case "runtime.models.status": {
          if (local.pull?.state === "pulling") {
            const percent = Math.min(100, (local.pull.percent ?? 0) + 20);
            local.pull = percent >= 100 ? { model: local.pull.model, state: "done" } : { ...local.pull, phase: "downloading", percent };
            if (local.pull.state === "done" && !local.models.includes(local.pull.model)) {
              local.models.push(local.pull.model);
              local.sizes[local.pull.model] = "4.1 GB";
            }
          }
          return ok({ embeddingModel: "demo/embed:latest", ...(local.pull ? { pull: { ...local.pull } } : {}) });
        }
        case "runtime.models.pull":
          local.pull = { model: String(payload?.model ?? ""), state: "pulling", phase: "starting" };
          return ok({ embeddingModel: "demo/embed:latest", pull: { ...local.pull } });
        case "runtime.models.cancel":
          if (local.pull?.state === "pulling") local.pull = { model: local.pull.model, state: "cancelled" };
          return ok({ embeddingModel: "demo/embed:latest", ...(local.pull ? { pull: { ...local.pull } } : {}) });
        case "runtime.models.remove":
          local.models = local.models.filter((model) => model !== payload?.model);
          return ok({ embeddingModel: "demo/embed:latest" });
        case "runtime.models.engine.start": return ok({ embeddingModel: "demo/embed:latest", engine: "ready" });
        case "runtime.settings.set": {
          for (const [id,value] of Object.entries((payload?.values ?? {}) as Record<string,string>)) {
            const row=settings.find(s=>s.id===id); if(row)row.value=value;
          }
          return ok({settings:structuredClone(settings),webhookChannels:["102"],busy:false,error:""});
        }
        case "communications.channels": return ok({ channels: [
          { id: "123", name: "reading-room", guildId: "900", guildName: "Example server" },
          { id: "124", name: "music", guildId: "900", guildName: "Example server" },
          { id: "125", name: "general", guildId: "901", guildName: "Second server" },
        ] });
        case "communications.history": return ok({ channelId: String(payload?.channel_id), messages: conversation, before: "" });
        case "communications.link.groups":
          return ok({ groups: [
            { id: "120363000000000001@g.us", name: "ningo", members: 1 },
            { id: "120363000000000002@g.us", name: "Reading club", members: 14 },
          ] });
        case "communications.link.status":
        case "communications.link.start":
        case "communications.link.reset": {
          const entry = status.services.whatsapp;
          if (type === "communications.link.reset" && entry) {
            entry.state = "waiting_for_link";
            entry.status = "error";
          }
          const state = entry?.state ?? "unknown";
          return ok({ state, ...(state === "waiting_for_link" ? { image: FAKE_CODE } : {}) });
        }
        case "communications.send": {
          conversation = [...conversation, { id: String(101 + conversation.length), author: "Ningo", bot: true, content: String(payload?.content ?? ""), timestamp: new Date().toISOString(), edited: false, replyTo: "", attachments: payload?.attachment_url ? ["meme"] : [] }];
          return ok({ sent: true, skipped: false });
        }
        case "services.set": {
          const entry = status.services[String(payload?.name)];
          if (!entry) return { ok: false, error: { code: "orchestrator", message: "unknown service" } };
          const enabled = payload?.enabled === true;
          entry.enabled = enabled;
          if (!enabled) entry.status = "disabled";
          else if (entry.status === "disabled") entry.status = STATUS.services[String(payload?.name)]?.status ?? "ok";
          return ok({ name: String(payload?.name), enabled });
        }
        case "scheduler.jobs.set": {
          const job = status.jobs?.find((candidate) => candidate.name === payload?.name);
          if (!job) return { ok: false, error: { code: "orchestrator", message: "unknown job" } };
          if (typeof payload?.enabled === "boolean") job.enabled = payload.enabled;
          if (typeof payload?.interval === "string") {
            const original = STATUS.jobs?.find((candidate) => candidate.name === job.name);
            const custom = payload.interval !== "default";
            job.intervalOverride = custom;
            job.interval = custom ? payload.interval.replace(/^(\d+)m$/, "$1m0s").replace(/^(\d+)h$/, "$1h0m0s") : original?.interval ?? "";
          }
          return ok({ name: job.name, enabled: job.enabled, interval: job.interval, intervalOverride: job.intervalOverride });
        }
        case "ningo.status":
          return ok(status);
        case "memory.status":
          return ok({ ...MEMORY_STATUS, pending: experiences.filter((entry) => !entry.outcome).length, resolved: experiences.filter((entry) => !!entry.outcome).length });
        case "memory.stats":
          return ok(MEMORY_SCORE);
        case "memory.list": {
          const state = payload?.state ?? "all";
          const rows = experiences.filter((entry) => state === "all" || (state === "pending" ? !entry.outcome : !!entry.outcome));
          const offset = Number(payload?.offset) || 0;
          const limit = Number(payload?.limit) || 20;
          return ok({ experiences: rows.slice(offset, offset + limit).map(({ context, ...entry }) => entry), total: rows.length, offset, limit });
        }
        case "memory.get": {
          const entry = experiences.find((entry) => entry.ref === payload?.ref);
          return ok(entry ? { found: true, experience: structuredClone(entry) } : { found: false });
        }
        case "memory.resolve": {
          const entry = experiences.find((entry) => entry.ref === payload?.ref);
          if (!entry) return ok({ found: false, resolved: false, outcome: "" });
          if (entry.outcome) return ok({ found: true, resolved: false, outcome: entry.outcome });
          entry.outcome = typeof payload?.observed === "string"
            ? (entry.detail.toLowerCase() === payload.observed.trim().toLowerCase() ? "accepted" : "replaced") : String(payload?.outcome);
          entry.resolvedAt = new Date().toISOString();
          entry.feedback = { code: "recorded", message: "" };
          return ok({ found: true, resolved: true, outcome: entry.outcome });
        }
        case "knowledge.status":
          return ok({
            ...KNOWLEDGE_STATUS,
            documents: documents.length,
            chunks: documents.reduce((sum, document) => sum + document.chunks, 0),
            embedded: documents.reduce((sum, document) => sum + document.embedded, 0),
            pendingEmbeddings: documents.reduce((sum, document) => sum + document.chunks - document.embedded, 0),
          });
        case "knowledge.search":
          return ok(searchFor(String(payload?.query ?? "")));
        case "meme.status":
          return ok(MEME_STATUS);
        case "digest.link.read": {
          const reply = fakePost(String(payload?.url ?? ""));
          if (reply.status === "ok") posts.set(reply.post.documentId, reply.post);
          return ok(reply);
        }
        case "knowledge.ingest": {
          if (payload?.mode === "post") {
            const post = posts.get(String(payload.post ?? ""));
            if (!post) return { ok: false, error: { code: "orchestrator", message: "that post is not held" } };
            const existing = documents.some((document) => document.id === post.documentId);
            documents = [
              {
                id: post.documentId, title: post.caption.split("\n")[0] ?? post.url, kind: "post", reference: post.url,
                tags: [], chunks: 1, embedded: 0, updatedAt: new Date().toISOString(),
              },
              ...documents.filter((document) => document.id !== post.documentId),
            ];
            const saved: IngestResult = {
              count: 1, created: existing ? 0 : 1, updated: existing ? 1 : 0, unchanged: 0, chunks: 1, degraded: false, reason: "",
            };
            return ok(saved);
          }
          const body = String(payload?.body ?? payload?.content ?? payload?.address ?? "");
          const id = String(payload?.source_id ?? `${String(payload?.kind ?? "note")}:added-${documents.length + 1}`);
          const existing = documents.some((document) => document.id === id);
          documents = [
            {
              id,
              title: String(payload?.title ?? "A new document"),
              kind: String(payload?.kind ?? "note"),
              reference: String(payload?.source_ref ?? ""),
              tags: Array.isArray(payload?.tags) ? payload.tags.map(String) : [],
              chunks: Math.max(1, Math.ceil(body.length / 900)),
              embedded: 0,
              updatedAt: new Date().toISOString(),
            },
            ...documents.filter((document) => document.id !== id),
          ];
          const result: IngestResult = {
            count: 1, created: existing ? 0 : 1, updated: existing ? 1 : 0, unchanged: 0,
            chunks: Math.max(1, Math.ceil(body.length / 900)), degraded: false, reason: "",
          };
          return ok(result);
        }
        case "knowledge.list": {
          const limit = Math.min(Math.max(Number(payload?.limit) || 20, 1), 100);
          const offset = Math.max(Number(payload?.offset) || 0, 0);
          const withText = payload?.include_text === true && (limit === 1 || typeof payload?.source_id === "string");
          // A search hit's document is read by its id.
          const shown = typeof payload?.source_id === "string" ? documents.filter((document) => document.id === payload.source_id) : documents;
          const page: KnowledgeDocuments = {
            documents: shown
              .slice(offset, offset + limit)
              .map((document) => (withText ? { ...document, text: FAKE_TEXT, truncated: false } : document)),
            total: documents.length,
            offset,
          };
          return ok(page);
        }
        case "knowledge.classify": {
          const id = String(payload?.source_id ?? "");
          if (!documents.some((document) => document.id === id)) return { ok: false, error: { code: "orchestrator", message: "knowledge document not found" } };
          let state = classifications.get(id) ?? {
            sourceId: id, revision: "0".repeat(64),
            form: { value: null, origin: "user_required", classifierId: "", classifierVersion: "", reason: "classification_missing" },
            contentKind: { value: null, origin: "user_required", classifierId: "", classifierVersion: "", reason: "classification_missing" },
            needsUserInput: true,
            questions: [
              { field: "form", question: "What is the form of this document?", allowedValues: ["book", "article", "note", "conversation", "meme"] },
              { field: "content_kind", question: "What type of content does this document contain?", allowedValues: ["news", "reference", "research", "narrative"] },
            ],
          } satisfies KnowledgeClassification;
          if (payload?.answers && typeof payload.answers === "object") {
            if (payload.revision !== state.revision) return { ok: false, error: { code: "orchestrator", message: "classification_conflict: reload the questions" } };
            state = structuredClone(state);
            const answers = payload.answers as Record<string, string>;
            for (const question of state.questions) {
              const value = answers[question.field];
              if (!value || !question.allowedValues.includes(value)) continue;
              const axis = question.field === "form" ? "form" : "contentKind";
              state[axis] = { value, origin: "user", classifierId: "", classifierVersion: "", reason: "" };
            }
            state.questions = state.questions.filter((question) => !(question.field === "form" ? state.form.value : state.contentKind.value));
            state.needsUserInput = state.questions.length > 0;
            state.revision = (BigInt(`0x${state.revision}`) + 1n).toString(16).padStart(64, "0");
          }
          classifications.set(id, state);
          return ok(structuredClone(state));
        }
        case "knowledge.delete": {
          const before = documents.length;
          documents = documents.filter((document) => document.id !== payload?.id);
          const result: DeleteResult = { deleted: documents.length < before };
          return ok(result);
        }
        case "knowledge.reindex": {
          let budget = Math.min(64, Math.max(1, Number(payload?.limit) || 32));
          let embedded = 0;
          documents = documents.map((document) => {
            const added = Math.min(budget, document.chunks - document.embedded);
            budget -= added;
            embedded += added;
            return { ...document, embedded: document.embedded + added };
          });
          const remaining = documents.reduce((sum, document) => sum + document.chunks - document.embedded, 0);
          const result: ReindexResult = { embedded, remaining, degraded: false, reason: "" };
          return ok(result);
        }
        case "digest.status":
          return ok(DIGEST_STATUS);
        case "digest.items": {
          const limit = Math.min(Math.max(Number(payload?.limit) || 8, 1), 50);
          const offset = Math.max(Number(payload?.offset) || 0, 0);
          const all = digestItems();
          const items = all.slice(offset, offset + limit);
          const page: DigestPage = {
            items,
            total: all.length,
            offset,
            more: Math.max(all.length - offset - items.length, 0),
            updated: minutesAgo(6),
          };
          return ok(page);
        }
        case "meme.pool.refresh": return ok({ added: 3, sources: ["example-source"] });
        case "meme.list": {
          const scope = payload?.scope === "sent" ? "sent" : "unsent";
          const limit = Number(payload?.limit) || 12;
          const offset = Number(payload?.offset) || 0;
          const all = memes(scope);
          const page: MemePage = { total: all.length, offset, memes: all.slice(offset, offset + limit) };
          return ok(page);
        }
        case "meme.hashtag.tag":
          return ok({ source_url: payload?.url ?? "", hashtag: payload?.hashtag ?? "", embedded: true });
      }
      return ok({});
    },

    // The folder of a saved post; in the preview there is no file manager to open.
    async openSavedPost() {
      await sleep(delayMs);
      return ok({ opened: true });
    },

    async savePostLocally() {
      await sleep(delayMs);
      return ok({ files: 3 });
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
