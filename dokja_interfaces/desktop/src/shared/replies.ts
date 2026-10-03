// What the main process hands to the renderer for each action: a projection of the orchestrator
// reply with only the fields a card reads, so channel IDs, provider profiles and other detail
// never reach the screen.

export type ServiceState = {
  status: string;
  detail: string;
  enabled?: boolean;
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

// An item of the digest. The address is not here: nothing in the app opens a link yet, so the
// screen does not get one.
export type DigestItem = {
  id: string;
  title: string;
  summary: string;
  source: string;
  // RFC 3339, or empty when the source gave no date
  published: string;
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

export type CommunicationChannel = { id: string; name: string };
export type CommunicationMessage = {
  id: string; author: string; bot: boolean; content: string; timestamp: string;
  edited: boolean; replyTo: string; attachments: string[];
};
export type CommunicationHistory = { channelId: string; messages: CommunicationMessage[]; before: string };
export type CommunicationSend = { off?: false; sent: boolean; skipped: boolean };
