import { isRecord, text } from "../../shared/records.js";
import type { KnowledgeHit, KnowledgeSearch, KnowledgeStatus, Off } from "../../shared/replies.js";

export type Similarity = "on" | "off" | "down";

const num = (value: unknown): number =>
  typeof value === "number" && Number.isFinite(value) ? value : 0;

// The reply crossed a process boundary, so its shape is checked here too.
export function parseKnowledgeStatus(value: unknown): KnowledgeStatus | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (typeof value.documents !== "number") return undefined;
  return {
    documents: num(value.documents),
    chunks: num(value.chunks),
    embedded: num(value.embedded),
    pendingEmbeddings: num(value.pendingEmbeddings),
    embedModel: text(value.embedModel),
    embedderReachable: typeof value.embedderReachable === "boolean" ? value.embedderReachable : null,
  };
}

function parseHit(value: unknown): KnowledgeHit | undefined {
  if (!isRecord(value)) return undefined;
  return {
    rank: num(value.rank),
    title: text(value.title),
    heading: text(value.heading),
    kind: text(value.kind),
    sourceRef: text(value.sourceRef),
    tags: Array.isArray(value.tags) ? value.tags.map(text).filter((tag) => tag !== "") : [],
    text: text(value.text),
    score: typeof value.score === "number" && Number.isFinite(value.score) ? value.score : null,
    relevant: value.relevant === true,
  };
}

export function parseKnowledgeSearch(value: unknown): KnowledgeSearch | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (!Array.isArray(value.hits)) return undefined;
  return {
    query: text(value.query),
    hits: value.hits.map(parseHit).filter((hit): hit is KnowledgeHit => hit !== undefined),
    relevantCount: num(value.relevantCount),
    threshold: num(value.threshold),
    degraded: value.degraded === true,
    reason: text(value.reason),
  };
}

// No answer from the embedder is "down"; a service that does not say has similarity switched off.
export function similarityOf(status: KnowledgeStatus): Similarity {
  if (status.embedderReachable === null) return "off";
  return status.embedderReachable ? "on" : "down";
}
