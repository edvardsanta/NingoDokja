import type { StatusReply } from "../shared/replies.js";
import type { Transport } from "../shared/transport.js";

// Made-up data for `pnpm dev:web`, where there is no orchestrator and no shell.
const STATUS: StatusReply = {
  status: "degraded",
  services: {
    meme: { status: "ok", detail: "" },
    chat_ai: { status: "error", detail: "connection refused" },
    book: { status: "unchecked", detail: "" },
    memory: { status: "disabled", detail: "", enabled: false },
    scheduler: { status: "stopped", detail: "no announce for 12m (stopped?)" },
  },
};

export function createFakeTransport(delayMs = 400): Transport {
  return {
    async request(type) {
      await new Promise((resolve) => setTimeout(resolve, delayMs));
      if (type === "ningo.status") return { ok: true, result: STATUS };
      return {
        ok: false,
        error: { code: "denied", message: "not available in the browser preview" },
      };
    },
  };
}
