import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type RefObject } from "react";
import type { Transport } from "../../shared/transport.js";
import type { ArchivedTurn, ArchivedTurnState } from "../../shared/replies.js";
import { isRecord } from "../../shared/records.js";
import type { MessageId } from "../i18n/i18n.js";
import type { TransportError } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { Reading } from "../discussion_context.js";
import { Problem } from "./card_frame.js";

// A send as the screen shows it, named by its send id. The archive's turn is the truth whenever the
// screen has it; "unknown" is a send whose outcome the screen could not read back.
export type Turn = {
  sendId: string;
  id: string;
  content: string;
  reply: string;
  state: ArchivedTurnState | "unknown";
  reason: string;
};

export type Conversation = {
  // the archive's id once the conversation has been started there, empty before the first message
  id: string;
  turns: readonly Turn[];
  // the reply written so far while it is on its way, when the shell can stream it; never the reply
  preview: string;
  draft: string;
  setDraft: (draft: string) => void;
  busy: boolean;
  error: TransportError | undefined;
  // the archive could not start the conversation, so the last message was given back unsent
  unkept: boolean;
  field: RefObject<HTMLTextAreaElement | null>;
  send: (content: string) => Promise<void>;
  retry: () => void;
};

const fromArchive = (turn: ArchivedTurn): Turn => ({ sendId: turn.sendId, id: turn.id, content: turn.content, reply: turn.reply, state: turn.state, reason: turn.reason });

function newSendId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `send-${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
}

// What a send still needs: a turn that ended without a reply is retried by the owner, a send whose
// outcome is unknown is sent again under its own send id (the archive answers it once), and one still
// on its way is only read again.
export function retryable(turn: Turn | undefined): boolean {
  return turn !== undefined && turn.state !== "completed";
}

// One conversation with Ningo, kept by the orchestrator's archive: it is started there with the first
// message, every message carries its own send id, and after a failure the screen reads the archive
// back instead of guessing. A remount (a new conversation) drops whatever was on its way.
export function useConversation(transport: Transport, reading?: Reading): Conversation {
  const id = useRef("");
  const [conversationId, setConversationId] = useState("");
  const [draft, setDraft] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<TransportError>();
  const [unkept, setUnkept] = useState(false);
  const [preview, setPreview] = useState("");
  const field = useRef<HTMLTextAreaElement>(null);
  // Only this conversation's previews, and only while its turn is on its way.
  useEffect(() => {
    if (!busy || !transport.onChatPreview) return undefined;
    return transport.onChatPreview((each) => {
      if (id.current && each.conversationId === id.current) setPreview(each.text);
    });
  }, [busy, transport]);

  const settle = (sendId: string, change: (turn: Turn) => Turn) =>
    setTurns((previous) => previous.map((turn) => (turn.sendId === sendId ? change(turn) : turn)));

  // The archive's turns replace the screen's; a send it does not hold yet stays as the screen knew it.
  const readBack = async (sendId: string) => {
    const read = await transport.request("conversation.read", { conversation_id: id.current });
    if (!read.ok || !isRecord(read.result) || !Array.isArray(read.result.turns)) {
      settle(sendId, (turn) => (turn.state === "pending" ? { ...turn, state: "unknown" } : turn));
      return;
    }
    const archived = (read.result.turns as ArchivedTurn[]).map(fromArchive);
    setTurns((previous) => [...archived, ...previous.filter((turn) => !archived.some((each) => each.sendId === turn.sendId))]);
  };

  const dispatch = async (sendId: string, payload: Record<string, unknown>) => {
    setBusy(true); setError(undefined);
    try {
      const answer = await transport.request("message.created", { conversation_id: id.current, ...payload }, { timeoutMs: 120_000 });
      const turn = answer.ok && isRecord(answer.result) && isRecord(answer.result.turn) ? (answer.result.turn as unknown as ArchivedTurn) : undefined;
      if (turn) {
        settle(sendId, () => fromArchive(turn));
        return;
      }
      setError(answer.ok ? { code: "unexpected", message: "The assistant returned no reply." } : answer.error);
      await readBack(sendId);
    } catch {
      setError({ code: "unavailable", message: "The assistant could not be reached." });
      await readBack(sendId).catch(() => settle(sendId, (turn) => ({ ...turn, state: "unknown" })));
    } finally { setBusy(false); setPreview(""); field.current?.focus(); }
  };

  const send = async (content: string) => {
    const message = content.trim();
    if (!message || busy) return;
    setDraft(""); setError(undefined); setUnkept(false);
    if (!id.current) {
      setBusy(true);
      const started = await transport.request("conversation.start", reading ? { reading_id: reading.id } : {}).catch(() => undefined);
      const conversation = started?.ok && isRecord(started.result) && isRecord(started.result.conversation) ? started.result.conversation : undefined;
      if (!conversation || typeof conversation.id !== "string") {
        // Nothing is answered unless it is kept, so the message goes back to the owner unsent.
        setBusy(false); setDraft(message); setUnkept(true);
        return;
      }
      id.current = conversation.id;
      setConversationId(conversation.id);
    }
    const sendId = newSendId();
    setTurns((previous) => [...previous, { sendId, id: "", content: message, reply: "", state: "pending", reason: "" }]);
    await dispatch(sendId, { content: message, send_id: sendId });
  };

  // Only the latest send is retried, and never by sending its text as a new message.
  const retry = () => {
    const latest = turns.at(-1);
    if (busy || !retryable(latest) || !latest) return;
    if (latest.state === "pending") { void readBack(latest.sendId); return; }
    settle(latest.sendId, (turn) => ({ ...turn, state: "pending", reason: "" }));
    if ((latest.state === "failed" || latest.state === "interrupted") && latest.id) void dispatch(latest.sendId, { retry_turn_id: latest.id });
    else void dispatch(latest.sendId, { content: latest.content, send_id: latest.sendId });
  };
  return { id: conversationId, turns, preview, draft, setDraft, busy, error, unkept, field, send, retry };
}

// What was said, newest at the bottom, and what is happening now. The end is kept in view as the
// conversation grows. A send without a reply says why, and only the latest can be sent again.
export function Transcript({ conversation }: { conversation: Conversation }) {
  const t = useTranslate();
  const { turns, busy, error, preview } = conversation;
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: "end" });
  }, [turns.length, busy, error, preview]);
  const latest = turns.at(-1);
  return <>
    <ol className="assistant-turns" aria-live="polite">{turns.flatMap((turn) => [
      <li key={`${turn.sendId}-user`} data-role="user"><strong>{t("assistant_you")}</strong><p>{turn.content}</p></li>,
      ...(turn.state === "completed" ? [<li key={`${turn.sendId}-reply`} data-role="assistant"><strong>{t("assistant_name")}</strong><p>{turn.reply}</p></li>] : []),
      ...(turn !== latest && turn.state !== "completed" ? [<li key={`${turn.sendId}-none`} className="detail" data-role="assistant">{t("assistant_no_reply", { reason: turn.reason || t("assistant_no_reply_unknown") })}</li>] : []),
    ])}</ol>
    {/* Outside the live list, so a reader of the screen hears the reply once, when it lands. */}
    {busy && preview && <ol className="assistant-turns"><li className="assistant-preview" data-role="assistant" aria-busy="true"><strong>{t("assistant_name")}</strong><p>{preview}</p></li></ol>}
    {busy && !preview && <p role="status">{t("assistant_thinking")}</p>}
    {!busy && latest?.state === "pending" && <p role="status">{t("assistant_still_pending")}</p>}
    {!busy && latest && (latest.state === "failed" || latest.state === "interrupted") && !error && <p role="alert">{t(latest.state === "interrupted" ? "assistant_interrupted" : "assistant_no_reply", { reason: latest.reason || t("assistant_no_reply_unknown") })}</p>}
    {conversation.unkept && <p role="alert">{t("assistant_history_unavailable")}</p>}
    {error && <Problem error={error}/>}
    {!busy && retryable(latest) && <button type="button" onClick={conversation.retry}>{t(latest?.state === "pending" ? "assistant_check_again" : "assistant_retry")}</button>}
    <div ref={end} />
  </>;
}

// Where the owner writes. Enter sends and Shift+Enter breaks the line.
export function Composer({ conversation, placeholder, hint, keyshortcuts }: { conversation: Conversation; placeholder: string; hint?: string; keyshortcuts?: string }) {
  const t = useTranslate();
  const { id, draft, setDraft, busy, field, send } = conversation;
  const fieldId = `assistant-prompt-${id}`;
  const hintId = `${fieldId}-hint`;
  const submit = (event: FormEvent) => { event.preventDefault(); void send(draft); };
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    void send(draft);
  };
  return <form className="composer" onSubmit={submit}>
    <label htmlFor={fieldId}>{t("assistant_input_label")}</label>
    <textarea id={fieldId} ref={field} value={draft} aria-keyshortcuts={keyshortcuts} maxLength={2000} rows={3} disabled={busy} aria-describedby={hint ? hintId : undefined}
      onChange={(event) => setDraft(event.target.value)} onKeyDown={onKeyDown} placeholder={placeholder}/>
    <button type="submit" className="primary" disabled={busy || draft.trim() === ""}>{t("assistant_send")}</button>
    {hint && <p className="detail composer-hint" id={hintId}>{hint}</p>}
  </form>;
}

// A conversation about one reading, inline under it: the reading's own suggestions send at once.
// The questions a reading's discussion can open with; each is sent as it is.
export const READING_PROMPTS: readonly MessageId[] = ["assistant_prompt_summary", "assistant_prompt_question", "assistant_prompt_critique"];
