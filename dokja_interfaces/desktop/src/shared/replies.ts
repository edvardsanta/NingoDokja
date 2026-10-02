// What the main process hands to the renderer for each action: a projection of the orchestrator
// reply with only the fields a card reads, so channel IDs, provider profiles and other detail
// never reach the screen.

export type ServiceState = {
  status: string;
  detail: string;
  enabled?: boolean;
};

export type StatusReply = {
  status: string;
  services: Record<string, ServiceState>;
};

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
