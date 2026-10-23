import { isRecord } from "../shared/records.js";
import { OrchestratorError, buildEnvelope, type OrchestratorReply } from "./orchestrator_client.js";

// The longest text a preview may carry: a reply is never longer.
const MAX_PREVIEW_CHARS = 65_536;
// The longest line the stream may carry. Each preview repeats the text so far, so a long reply streams
// many times its own size: lines are bounded, and the deadline bounds the stream.
const MAX_LINE_BYTES = 512 * 1024;

// The bridge cannot take the turn and never saw it: it refused the connection, or it has no stream
// route (an older orchestrator). The turn can go over the request socket instead.
export class BridgeMissing extends Error {}

type BridgeOptions = {
  // The orchestrator's HTTP bridge on this machine, for example http://localhost:8091.
  endpoint: string;
  token: string;
  fetch?: typeof fetch;
};

// A Talk turn sent to the orchestrator's bridge, which answers with the reply written so far while
// Ningo writes, then with the response the request socket would give. The previews are handed on as
// they come and are never the reply; the reply is the last line, read as a request reply is.
export class BridgeChat {
  private readonly url: string;
  private readonly token: string;
  private readonly fetch: typeof fetch;

  constructor(options: BridgeOptions) {
    this.url = new URL("/orchestrator/chat/stream", options.endpoint).toString();
    this.token = options.token;
    this.fetch = options.fetch ?? fetch;
  }

  async stream(payload: Record<string, unknown>, timeoutMs: number, onPreview: (text: string) => void): Promise<OrchestratorReply> {
    let response: Response;
    try {
      response = await this.fetch(this.url, {
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: `Bearer ${this.token}` },
        body: JSON.stringify(buildEnvelope("message.created", payload, "")),
        signal: AbortSignal.timeout(timeoutMs),
      });
    } catch (error) {
      if (error instanceof Error && error.name === "TimeoutError") throw new OrchestratorError("timeout", `no answer within ${timeoutMs} ms`);
      if (refused(error)) throw new BridgeMissing("the orchestrator's bridge is not listening");
      throw new OrchestratorError("unavailable", "the orchestrator's bridge did not answer");
    }
    if (response.status === 404) throw new BridgeMissing("the orchestrator's bridge has no chat stream");
    if (response.status === 401) throw new OrchestratorError("orchestrator", "the orchestrator's bridge refused the desktop's credential");
    if (!response.ok || !response.body) throw new OrchestratorError("orchestrator", `the orchestrator's bridge answered ${response.status}`);

    const decoder = new TextDecoder();
    let pending = "";
    const reader = response.body.getReader();
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        pending += decoder.decode(value, { stream: true });
        let end: number;
        while ((end = pending.indexOf("\n")) >= 0) {
          const line = pending.slice(0, end).trim();
          pending = pending.slice(end + 1);
          if (line === "") continue;
          const reply = readLine(line, onPreview);
          if (reply) return reply;
        }
        if (Buffer.byteLength(pending) > MAX_LINE_BYTES) throw new OrchestratorError("orchestrator", "the reply is too long");
      }
    } catch (error) {
      if (error instanceof OrchestratorError) throw error;
      if (error instanceof Error && error.name === "TimeoutError") throw new OrchestratorError("timeout", `no answer within ${timeoutMs} ms`);
      throw new OrchestratorError("unavailable", "the stream from the orchestrator stopped");
    } finally {
      reader.releaseLock();
    }
    throw new OrchestratorError("orchestrator", "the stream ended without a reply");
  }
}

// Nothing listens at the address: the turn never reached the orchestrator.
function refused(error: unknown): boolean {
  const cause = error instanceof Error ? (error.cause as { code?: unknown } | undefined) : undefined;
  return cause?.code === "ECONNREFUSED";
}

// A preview is handed on; the response ends the stream. Anything else is not what the bridge sends.
function readLine(line: string, onPreview: (text: string) => void): OrchestratorReply | undefined {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch {
    throw new OrchestratorError("orchestrator", "the stream is not valid JSON");
  }
  if (!isRecord(value)) throw new OrchestratorError("orchestrator", "the stream is not valid JSON");
  if (typeof value.preview === "string") {
    onPreview(value.preview.slice(0, MAX_PREVIEW_CHARS));
    return undefined;
  }
  if (typeof value.status !== "string") throw new OrchestratorError("orchestrator", "the reply has no status");
  return {
    status: value.status,
    ...(value.result !== undefined ? { result: value.result } : {}),
    ...(typeof value.error === "string" ? { error: value.error } : {}),
  };
}
