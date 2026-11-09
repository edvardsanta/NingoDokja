import { randomUUID } from "node:crypto";
import { OrchestratorError, type OrchestratorReply } from "./orchestrator_client.js";
import type { Presence } from "./presence.js";

// The shell owns the microphone session. Only an explicitly started session can
// transcribe, one utterance at a time, and the permission policy lets the app
// window open the microphone only while a session is listening.
export class DesktopVoiceGate {
  private session?: { id: string; busy: boolean };
  get listening(): boolean { return this.session !== undefined; }
  // A new start replaces any earlier session, so a reloaded window is never locked out.
  start(): string {
    const id = randomUUID();
    this.session = { id, busy: false };
    return id;
  }
  // Answers voice.desktop.start the way the orchestrator answers a single-domain request.
  startReply(): OrchestratorReply {
    return { status: "ok", result: { domain: "chat", result: { voice_session_id: this.start() } } };
  }
  stopReply(id: unknown): OrchestratorReply {
    this.stop(typeof id === "string" ? id : undefined);
    return { status: "ok", result: { domain: "chat", result: { stopped: true } } };
  }
  stop(id?: string): void { if (!id || id === this.session?.id) this.session = undefined; }
  async turn(id: unknown, request: () => Promise<OrchestratorReply>): Promise<OrchestratorReply> {
    const held = this.session;
    if (!held || id !== held.id || held.busy) throw new OrchestratorError("denied", "No available microphone session.");
    held.busy = true;
    try {
      const reply = await request();
      if (this.session !== held) throw new OrchestratorError("cancelled", "Microphone session ended.");
      return reply;
    } finally { held.busy = false; }
  }
}

// A spoken message is sent seconds after the press that opened the microphone, past the
// presence window. The listening session, which only a real press opens, stands in for that press
// for sending a message (and starting its conversation); every other change needs a fresh press.
const SPOKEN_SENDS: ReadonlySet<string> = new Set(["conversation.start", "message.created"]);

export function withVoicePresence(presence: Pick<Presence, "recent">, gate: Pick<DesktopVoiceGate, "listening">): Pick<Presence, "recent"> {
  return { recent: (action) => presence.recent(action) || (action !== undefined && SPOKEN_SENDS.has(action) && gate.listening) };
}
