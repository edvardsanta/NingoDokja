import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type RefObject } from "react";
import type { Transport } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import type { MessageId } from "../i18n/i18n.js";
import type { TransportError } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { Reading } from "../discussion_context.js";
import { Problem } from "./card_frame.js";

type Turn = { role: "user" | "assistant"; text: string };

export type Conversation = {
  id: string;
  turns: readonly Turn[];
  // the reply written so far while it is on its way, when the shell can stream it; never the reply
  preview: string;
  draft: string;
  setDraft: (draft: string) => void;
  busy: boolean;
  error: TransportError | undefined;
  notice: string;
  field: RefObject<HTMLTextAreaElement | null>;
  send: (content: string) => Promise<void>;
  retry: () => void;
};

// One conversation with Ningo: what was said, what is being written, and the one request on its way.
// The id is the screen's own, so the orchestrator keeps this conversation's history apart from any
// other (a reading's discussion, a new conversation).
export function useConversation(transport: Transport, reading?: Reading): Conversation {
  const t = useTranslate();
  const id = useRef(globalThis.crypto?.randomUUID?.() ?? `desktop-${Date.now()}-${Math.random().toString(36).slice(2)}`);
  const [draft, setDraft] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<TransportError>();
  const [notice, setNotice] = useState("");
  const [preview, setPreview] = useState("");
  const field = useRef<HTMLTextAreaElement>(null);
  // Only this conversation's previews, and only while its turn is on its way.
  useEffect(() => {
    if (!busy || !transport.onChatPreview) return undefined;
    return transport.onChatPreview((each) => {
      if (each.conversationId === id.current) setPreview(each.text);
    });
  }, [busy, transport]);
  const send = async (content: string) => {
    const message = content.trim();
    if (!message || busy) return;
    setTurns((previous) => [...previous, { role: "user", text: message }]);
    setDraft(""); setBusy(true); setError(undefined); setNotice("");
    try {
      const answer = await transport.request("message.created", {
        content: message,
        conversation_id: id.current,
        ...(reading ? { reading_id: reading.id } : {}),
      }, { timeoutMs: 120_000 });
      if (!answer.ok) { setError(answer.error); return; }
      const result = answer.result;
      if (!isRecord(result) || typeof result.reply !== "string" || result.reply.length === 0) {
        setError({ code: "unexpected", message: "The assistant returned no reply." }); return;
      }
      setTurns((previous) => [...previous, { role: "assistant", text: result.reply as string }]);
      setNotice(result.sessionNotice === "new" || result.sessionNotice === "reset" ? t("assistant_session_reset") : "");
    } catch {
      setError({ code: "unavailable", message: "The assistant could not be reached." });
    } finally { setBusy(false); setPreview(""); field.current?.focus(); }
  };
  const retry = () => void send(turns.at(-1)?.role === "user" ? turns.at(-1)!.text : draft);
  return { id: id.current, turns, preview, draft, setDraft, busy, error, notice, field, send, retry };
}

// What was said, newest at the bottom, and what is happening now. The end is kept in view as the
// conversation grows.
export function Transcript({ conversation }: { conversation: Conversation }) {
  const t = useTranslate();
  const { turns, busy, notice, error, preview } = conversation;
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView?.({ block: "end" });
  }, [turns.length, busy, error, preview]);
  return <>
    <ol className="assistant-turns" aria-live="polite">{turns.map((turn, index) => <li key={index} data-role={turn.role}><strong>{t(turn.role === "user" ? "assistant_you" : "assistant_name")}</strong><p>{turn.text}</p></li>)}</ol>
    {/* Outside the live list, so a reader of the screen hears the reply once, when it lands. */}
    {busy && preview && <ol className="assistant-turns"><li className="assistant-preview" data-role="assistant" aria-busy="true"><strong>{t("assistant_name")}</strong><p>{preview}</p></li></ol>}
    {busy && !preview && <p role="status">{t("assistant_thinking")}</p>}
    {notice && <p role="status">{notice}</p>}
    {error && <><Problem error={error}/><button type="button" disabled={busy} onClick={conversation.retry}>{t("assistant_retry")}</button></>}
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
