import type { ActionDefinition } from "./actions.js";
import { isRecord, text } from "../shared/records.js";
import type { MemoryExperience, MemoryExperiences, MemoryExperienceDetail, MemoryResolution } from "../shared/replies.js";

const cut = (value: unknown, limit: number) => text(value).slice(0, limit);
const integer = (value: unknown, max: number, fallback: number) =>
  typeof value === "number" && Number.isFinite(value) ? Math.max(0, Math.min(max, Math.trunc(value))) : fallback;
const probability = (value: unknown): number | null => typeof value === "number" && Number.isFinite(value) && value >= 0 && value <= 1 ? value : null;

function experience(raw: Record<string, unknown>): MemoryExperience {
  const feedback = isRecord(raw.feedback) ? raw.feedback : {};
  const context = typeof raw.context === "string" ? Array.from(raw.context.slice(0, 8000)).slice(0, 4000).join("") : undefined;
  return {
    ref: cut(raw.ref, 512), action: cut(raw.action, 64), detail: cut(raw.detail, 200), outcome: cut(raw.outcome, 32),
    predictedP: probability(raw.predicted_p), baselineP: probability(raw.baseline_p), matchedScore: probability(raw.matched_score),
    createdAt: cut(raw.created_at, 40), resolvedAt: cut(raw.resolved_at, 40),
    contextSnippet: cut(raw.context_snippet, 320), matchedSnippet: cut(raw.matched_snippet, 320),
    ...(context === undefined ? {} : { context }), contextTruncated: raw.context_truncated === true || (typeof raw.context === "string" && raw.context !== context),
    feedback: { code: cut(feedback.code, 40), message: cut(feedback.message, 800) },
  };
}

// The existing domain wrapper applies the off-service and domain checks in actions.ts.
export const memoryListPayload: ActionDefinition["payload"] = (raw) => {
  const state = raw.state ?? "all";
  if (state !== "all" && state !== "pending" && state !== "resolved") return undefined;
  return { state, limit: Math.max(1, integer(raw.limit, 20, 20)), offset: integer(raw.offset, 1_000_000, 0), include_context: true };
};
export function memoryPage(body: Record<string, unknown>): MemoryExperiences {
  if (!Array.isArray(body.experiences)) throw new Error("invalid experience page");
  return {
    experiences: body.experiences.filter(isRecord).slice(0, 100).map((raw) => {
      // A listing never returns full context, even if an older/untrusted service includes it.
      const { context, context_truncated, ...summary } = raw;
      return experience(summary);
    }),
    total: integer(body.total, Number.MAX_SAFE_INTEGER, 0), offset: integer(body.offset, 1_000_000, 0), limit: integer(body.limit, 20, 20),
  };
}
export const memoryGetPayload: ActionDefinition["payload"] = (raw) => {
  const ref = text(raw.ref).trim();
  return ref && Buffer.byteLength(ref) <= 512 && !/[\x00-\x1f\x7f]/.test(ref) ? { ref, include_context: true } : undefined;
};
export function memoryDetail(body: Record<string, unknown>): MemoryExperienceDetail {
  if (body.found === false) return { found: false };
  if (body.found !== true || typeof body.ref !== "string" || !body.ref) throw new Error("invalid experience detail");
  return { found: true, experience: experience(body) };
}

// Explicit feedback only. Expiry is a storage policy, never a user rejection.
export const memoryResolvePayload: ActionDefinition["payload"] = (raw) => {
  const ref = typeof raw.ref === "string" ? raw.ref.trim() : "";
  if (!ref || Buffer.byteLength(ref) > 512 || /[\x00-\x1f\x7f]/.test(ref)) return undefined;
  const observed = typeof raw.observed === "string" ? raw.observed.trim() : "";
  if (raw.outcome !== undefined && raw.observed !== undefined) return undefined;
  if (raw.observed !== undefined) {
    return observed && Array.from(observed).length <= 200 && !/[\p{Cc}]/u.test(observed) ? { ref, observed } : undefined;
  }
  return ["accepted", "replaced", "ignored"].includes(String(raw.outcome)) ? { ref, outcome: raw.outcome } : undefined;
};
export function memoryResolution(body: Record<string, unknown>): MemoryResolution {
  if (typeof body.found !== "boolean" || typeof body.resolved !== "boolean" || (body.resolved && !body.found)) throw new Error("invalid experience feedback reply");
  const outcome = cut(body.outcome, 32);
  if (body.found && !["accepted", "replaced", "ignored", "expired"].includes(outcome)) throw new Error("invalid experience outcome");
  return { found: body.found, resolved: body.resolved, outcome };
}
