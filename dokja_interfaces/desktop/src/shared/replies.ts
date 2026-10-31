// What the main process hands to the renderer for each action: a projection of the orchestrator
// reply with only the fields a card reads, so channel IDs, provider profiles and other detail
// never reach the screen.

export type ServiceState = {
  status: string;
  detail: string;
  enabled?: boolean;
  // An interface (Discord, WhatsApp) is listed with the services but carries its kind and a
  // state from the interface's own closed list ("connected", "waiting_for_link", ...).
  kind?: "interface";
  configured?: boolean;
  state?: string;
};

// One scheduled job as the orchestrator reports it. The times are RFC 3339 and empty when unknown.
export type JobState = {
  name: string;
  enabled: boolean;
  interval: string;
  intervalOverride: boolean;
  nextAt: string;
  lastAt: string;
  lastOutcome: string;
  lastError: string;
};

// The state of an interface that links to an account and, while it waits for the link, the picture
// of the code as a data URL. The code itself, as text, never reaches the screen.
export type InterfaceLink = { state: string; image?: string };

// A group the linked WhatsApp account takes part in, for the owner to allow by name.
export type LinkedGroup = { id: string; name: string; members: number };

export type StatusReply = {
  status: string;
  services: Record<string, ServiceState>;
  jobs?: JobState[];
};

// What the orchestrator says after a switch: the state it now holds, not the one that was asked for.
export type ServiceSwitched = { off?: false; name: string; enabled: boolean };
export type JobSwitched = { off?: false; name: string; enabled: boolean; interval: string; intervalOverride: boolean };

// The orchestrator understood the request but the operator switched that service off.
export type Off = { off: true; reason: string };

export type MemoryStatus = {
  off?: false;
  experiences: number;
  pending: number;
  resolved: number;
  expired: number;
  embedded: number;
  needsReindex: number;
  embedModel: string;
  embeddings: boolean;
  embedderReachable: boolean;
};

export type MemoryScore = {
  off?: false;
  scored: number;
  unscored: number;
  minScored: number;
  brierPrediction: number;
  brierBaseline: number;
  skill: number;
  beatsBaseline: boolean;
  enoughData: boolean;
};

export type KnowledgeStatus = {
  off?: false;
  documents: number;
  chunks: number;
  embedded: number;
  pendingEmbeddings: number;
  embedModel: string;
  // null when the service does not say (similarity is switched off)
  embedderReachable: boolean | null;
};

export type KnowledgeHit = {
  rank: number;
  title: string;
  heading: string;
  kind: string;
  sourceRef: string;
  sourceId: string;
  tags: string[];
  text: string;
  score: number | null;
  relevant: boolean;
};

export type KnowledgeSearch = {
  off?: false;
  query: string;
  hits: KnowledgeHit[];
  relevantCount: number;
  threshold: number;
  degraded: boolean;
  reason: string;
};

export type MemeStatus = {
  off?: false;
  status: string;
  unsent: number;
  sent: number;
};

export type MemeItem = {
  url: string;
  title: string;
  tags: string;
  source: string;
  createdAt: string;
  sentAt: string;
};

export type MemePage = {
  off?: false;
  total: number;
  offset: number;
  memes: MemeItem[];
};

// One source the feeds service follows: a plugin, with how its last runs went. The reason in
// `error` is a short fixed phrase from the service, never what the plugin printed.
export type DigestSource = {
  id: string;
  name: string;
  // ok, failed, pending, disabled, invalid, or unknown for anything else
  state: string;
  running: boolean;
  items: number;
  skipped: number;
  // RFC 3339, or empty when it never succeeded
  lastOk: string;
  error: string;
};

export type DigestStatus = {
  providers?: DigestSource[];
  off?: false;
  // false when the feeds service has no plugins directory at all
  configured: boolean;
  directoryError: string;
  ok: number;
  failed: number;
  pending: number;
  disabled: number;
  invalid: number;
  items: number;
  sources: DigestSource[];
};

// An item of the digest. Article addresses stay in the shell; optional media references
// are authorized by its preview gate and rendered only from the data URL it returns.
export type DigestItem = {
  previewText?: string;
  mediaUrl?: string;
  id: string;
  title: string;
  summary: string;
  source: string;
  // RFC 3339, or empty when the source gave no date
  published: string;
  // questions the item's own source suggests for discussing it; absent when it has none
  prompts?: string[];
};

export type DigestPage = {
  off?: false;
  items: DigestItem[];
  total: number;
  offset: number;
  // how many more items the digest holds after this page
  more: number;
  updated: string;
};

// What adding to the research base did. One document or many (a feed file is many): how many were
// new, how many replaced an earlier version of the same document, and how many were already there.
export type IngestResult = {
  off?: false;
  count: number;
  created: number;
  updated: number;
  unchanged: number;
  chunks: number;
  // true when the passages could not be indexed by meaning yet; they are still found by keywords
  degraded: boolean;
  reason: string;
};

// One document of the research base.
export type KnowledgeDocument = {
  id: string;
  title: string;
  kind: string;
  reference: string;
  tags: string[];
  chunks: number;
  embedded: number;
  updatedAt: string;
  // Only when one document was asked for with its text: rebuilt from its passages, and cut when long.
  text?: string;
  truncated?: boolean;
};

export type KnowledgeDocuments = {
  off?: false;
  documents: KnowledgeDocument[];
  total: number;
  offset: number;
};

export type DeleteResult = { off?: false; deleted: boolean };

// What indexing the waiting passages did: how many now have a vector, and how many still wait.
export type ReindexResult = {
  off?: false;
  embedded: number;
  remaining: number;
  degraded: boolean;
  reason: string;
};

// `retentionDays` is only there for a conversation an interface keeps the text of (WhatsApp): how
// long that text stays on this machine.
export type CommunicationChannel = { id: string; name: string; guildId?: string; guildName?: string; retentionDays?: number };
export type CommunicationMessage = {
  id: string; author: string; bot: boolean; content: string; timestamp: string;
  edited: boolean; replyTo: string; attachments: string[];
};
export type CommunicationHistory = { channelId: string; messages: CommunicationMessage[]; before: string };
export type CommunicationSend = { off?: false; sent: boolean; skipped: boolean };
export type MemeHashtagTag = { sourceUrl: string; hashtag: string; embedded: boolean };
// The learned hashtag closest to one meme; relevant is false when nothing is close enough.
export type MemeHashtagSuggestion = { relevant: boolean; hashtag: string };
// What a refresh of the meme pool brought: how many memes were new, and from which sources.
export type MemePoolRefresh = { added: number; sources: string[] };


export type ClassificationAxis = "form" | "content_kind";
export type ClassificationChoice = {
  value: string | null;
  origin: string;
  classifierId: string;
  classifierVersion: string;
  reason: string;
};
export type KnowledgeClassification = {
  off?: false;
  sourceId: string;
  revision: string;
  form: ClassificationChoice;
  contentKind: ClassificationChoice;
  needsUserInput: boolean;
  questions: { field: ClassificationAxis; question: string; allowedValues: string[] }[];
};

export type MemoryExperience = {
  ref: string;
  action: string;
  detail: string;
  outcome: string;
  predictedP: number | null;
  baselineP: number | null;
  matchedScore: number | null;
  createdAt: string;
  resolvedAt: string;
  contextSnippet: string;
  matchedSnippet: string;
  context?: string;
  contextTruncated: boolean;
  feedback: { code: string; message: string };
};
export type MemoryExperiences = { off?: false; experiences: MemoryExperience[]; total: number; offset: number; limit: number };
export type MemoryExperienceDetail = { off?: false; found: false } | { off?: false; found: true; experience: MemoryExperience };

export type MemoryResolution = { off?: false; found: boolean; resolved: boolean; outcome: string };

export type ChatReply = { off?: false; reply: string; sessionNotice: string };

// One send of an archived conversation and how it ended, as the archive keeps it. A pending turn is
// not proof a reply is coming; an interrupted one was cut by a restart and nobody knows its outcome.
export type ArchivedTurnState = "pending" | "completed" | "failed" | "interrupted";
export type ArchivedTurn = {
  id: string;
  seq: number;
  sendId: string;
  content: string;
  reply: string;
  state: ArchivedTurnState;
  reason: string;
  attempts: number;
  // Who answered, as the runtime reported it; empty when it reported nothing.
  provider: string;
  model: string;
  // The documents the reply cites; absent on a reply that cites none or was kept before they were.
  sources?: ArchivedTurnSource[];
};

export type ArchivedTurnSource = { number: number; id: string; title: string; reference: string; excerpt: string };

export type ArchivedConversation = {
  id: string;
  kind: "general" | "reading";
  readingId: string;
  title: string;
  updatedAt: string;
  revision: number;
  turns: number;
};

// The answer to an archived send: the turn as the archive now holds it, and whether the same send
// was answered before (a replay, never asked again).
export type ArchivedChatReply = { off?: false; reply: string; replayed: boolean; turn: ArchivedTurn };

export type ConversationStarted = { off?: false; conversation: ArchivedConversation };

// A fact a known person asked Ningo to keep. Where it was kept is told as a platform, never a chat.
export type KeptFact = { id: string; owner: boolean; person: string; content: string; place: string; createdAt: string };

export type KeptFacts = { off?: false; facts: KeptFact[] };

export type ConversationList = { off?: false; conversations: ArchivedConversation[]; next: string };

export type ConversationRead = { off?: false; conversation: ArchivedConversation; turns: ArchivedTurn[] };
