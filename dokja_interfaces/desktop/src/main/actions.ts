import type { ActionType } from "../shared/actions.js";
import { isRecord, text } from "../shared/records.js";
import type { ServiceState, StatusReply } from "../shared/replies.js";

type Projection = (result: unknown) => unknown;

const MAX_DETAIL_CHARS = 200;

// What the renderer receives for each action: only the fields its card reads.
export const PROJECTIONS: Record<ActionType, Projection> = {
  "ningo.status": projectStatus,
};

// The orchestrator answers with the result of the one domain that handled the event. A verbose
// reply, or an event that several domains handled, nests it under the domain name instead.
function domainResult(result: unknown, domain: string): Record<string, unknown> | undefined {
  if (!isRecord(result)) return undefined;
  const inner = result.result;
  if (!isRecord(inner)) return undefined;
  if (result.domain === domain) return inner;
  const nested = inner[domain];
  return isRecord(nested) ? nested : undefined;
}

function shorten(value: string): string {
  return value.length > MAX_DETAIL_CHARS ? `${value.slice(0, MAX_DETAIL_CHARS - 1)}…` : value;
}

function projectStatus(result: unknown): StatusReply {
  const system = domainResult(result, "system");
  if (!system) throw new Error("unexpected status reply");

  const services: Record<string, ServiceState> = {};
  if (isRecord(system.services)) {
    for (const [name, entry] of Object.entries(system.services)) {
      if (!isRecord(entry)) continue;
      services[name] = {
        status: text(entry.status),
        detail: shorten(text(entry.error) || text(entry.detail)),
        ...(typeof entry.enabled === "boolean" ? { enabled: entry.enabled } : {}),
      };
    }
  }
  return { status: text(system.status) || "ok", services };
}
