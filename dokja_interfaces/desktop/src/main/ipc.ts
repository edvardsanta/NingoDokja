import { ACTION_TYPES, type ActionType } from "../shared/actions.js";
import { isRecord } from "../shared/records.js";
import type { TransportErrorCode, TransportResult } from "../shared/transport.js";
import { PROJECTIONS } from "./actions.js";
import { OrchestratorError, type OrchestratorReply } from "./orchestrator_client.js";

type Orchestrator = {
  request(
    type: string,
    payload: Record<string, unknown>,
    timeoutMs: number,
  ): Promise<OrchestratorReply>;
};

export type RequestLimits = {
  defaultTimeoutMs: number;
  // The longest a request may wait, whatever the screen asks for.
  maxTimeoutMs: number;
};

const MAX_PAYLOAD_CHARS = 16 * 1024;

export function isAllowedAction(type: unknown): type is ActionType {
  return typeof type === "string" && (ACTION_TYPES as readonly string[]).includes(type);
}

function fail(code: TransportErrorCode, message: string): TransportResult {
  return { ok: false, error: { code, message } };
}

// Validates one request from the renderer and runs it. Nothing reaches the orchestrator unless
// the action is on the allow-list and the payload is a small plain object, and the screen only
// gets the projection of the reply that its action defines.
export async function handleRequest(
  raw: unknown,
  orchestrator: Orchestrator,
  limits: RequestLimits,
): Promise<TransportResult> {
  if (!isRecord(raw)) return fail("invalid", "the request must be an object");
  const { type } = raw;
  if (!isAllowedAction(type)) return fail("denied", "this action is not allowed");

  const payload = raw.payload ?? {};
  if (!isRecord(payload) || !fitsInPayload(payload)) {
    return fail("invalid", "the payload must be a small object");
  }

  try {
    const reply = await orchestrator.request(type, payload, pickTimeout(raw.options, limits));
    if (reply.status !== "ok") {
      return fail("orchestrator", reply.error || "the orchestrator refused the request");
    }
    return { ok: true, result: PROJECTIONS[type](reply.result) };
  } catch (error) {
    if (error instanceof OrchestratorError) return fail(error.code, error.message);
    return fail("unexpected", "the reply was not what this action expects");
  }
}

function fitsInPayload(payload: Record<string, unknown>): boolean {
  try {
    return JSON.stringify(payload).length <= MAX_PAYLOAD_CHARS;
  } catch {
    return false; // not serializable, for example a cycle
  }
}

function pickTimeout(options: unknown, limits: RequestLimits): number {
  const asked = isRecord(options) ? options.timeoutMs : undefined;
  const wanted =
    typeof asked === "number" && Number.isFinite(asked) ? asked : limits.defaultTimeoutMs;
  return Math.min(Math.max(Math.round(wanted), 1), limits.maxTimeoutMs);
}
