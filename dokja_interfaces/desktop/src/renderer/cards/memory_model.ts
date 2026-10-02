import { isRecord, text } from "../../shared/records.js";
import type { MemoryScore, MemoryStatus, Off } from "../../shared/replies.js";
import type { TransportResult } from "../../shared/transport.js";

export type Similarity = "on" | "off" | "down";

// What the score section says. "too-few" shows the numbers but never a verdict: the score is only
// worth judging once enough predictions have been scored.
export type ScoreView =
  | { kind: "nothing" }
  | { kind: "off"; reason: string }
  | { kind: "error"; message: string }
  | { kind: "too-few"; score: MemoryScore }
  | { kind: "judged"; score: MemoryScore };

export type MemoryView =
  | { off: true; reason: string }
  | { off: false; status: MemoryStatus; score: ScoreView };

const num = (value: unknown): number =>
  typeof value === "number" && Number.isFinite(value) ? value : 0;

// The reply crossed a process boundary, so its shape is checked here too.
export function parseMemoryStatus(value: unknown): MemoryStatus | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (typeof value.experiences !== "number") return undefined;
  return {
    experiences: num(value.experiences),
    pending: num(value.pending),
    resolved: num(value.resolved),
    expired: num(value.expired),
    embedded: num(value.embedded),
    needsReindex: num(value.needsReindex),
    embedModel: text(value.embedModel),
    embeddings: value.embeddings === true,
    embedderReachable: value.embedderReachable === true,
  };
}

export function parseMemoryScore(value: unknown): MemoryScore | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (typeof value.scored !== "number") return undefined;
  return {
    scored: num(value.scored),
    unscored: num(value.unscored),
    minScored: num(value.minScored),
    brierPrediction: num(value.brierPrediction),
    brierBaseline: num(value.brierBaseline),
    skill: num(value.skill),
    beatsBaseline: value.beatsBaseline === true,
    enoughData: value.enoughData === true,
  };
}

export function similarityOf(status: MemoryStatus): Similarity {
  if (!status.embeddings) return "off";
  return status.embedderReachable ? "on" : "down";
}

// The score is read after the status and may fail on its own without taking the card down.
export function scoreViewOf(result: TransportResult): ScoreView {
  if (!result.ok) return { kind: "error", message: result.error.message };
  const score = parseMemoryScore(result.result);
  if (!score) return { kind: "error", message: "unexpected reply" };
  if (score.off) return { kind: "off", reason: score.reason };
  if (score.scored === 0) return { kind: "nothing" };
  return score.enoughData ? { kind: "judged", score } : { kind: "too-few", score };
}
