import { useRef, useState, type FormEvent } from "react";
import type { Transport } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import type { MessageId } from "../i18n/i18n.js";
import type { TransportError } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { Problem } from "./card_frame.js";

type Turn = { role: "user" | "assistant"; text: string };
type Props = { transport: Transport; reading?: { id: string; title: string; source: string } };

export function AssistantConversation({ transport, reading }: Props) {
  const t = useTranslate();
  const conversationId = useRef(globalThis.crypto?.randomUUID?.() ?? `desktop-${Date.now()}-${Math.random().toString(36).slice(2)}`);
  const [draft, setDraft] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<TransportError>();
  const [notice, setNotice] = useState("");
  const field = useRef<HTMLTextAreaElement>(null);
  const send = async (content: string) => {
    const message = content.trim();
    if (!message || busy) return;
    setTurns((previous) => [...previous, { role: "user", text: message }]);
    setDraft(""); setBusy(true); setError(undefined); setNotice("");
    try {
      const answer = await transport.request("message.created", {
        content: message,
        conversation_id: conversationId.current,
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
    } finally { setBusy(false); field.current?.focus(); }
  };
  const submit = (event: FormEvent) => { event.preventDefault(); void send(draft); };
  const prompts: MessageId[] = reading
    ? ["assistant_prompt_summary", "assistant_prompt_question", "assistant_prompt_critique"]
    : ["assistant_prompt_open", "assistant_prompt_plan", "assistant_prompt_idea"];
  return <section className="assistant-conversation" aria-label={t("assistant_title")}>
    {reading && <><p className="note">{t("assistant_reading_context", { title: reading.title, source: reading.source || t("assistant_unknown_source") })}</p><p className="detail">{t("assistant_excerpt_limit")}</p></>}
    {turns.length === 0 && <div className="assistant-prompts">
      <p>{t(reading ? "assistant_reading_intro" : "assistant_intro")}</p>
      {prompts.map((id) => <button key={id} type="button" className="assistant-prompt" disabled={busy} onClick={() => void send(t(id))}>{t(id)}</button>)}
    </div>}
    <ol className="assistant-turns" aria-live="polite">{turns.map((turn, index) => <li key={index} data-role={turn.role}><strong>{t(turn.role === "user" ? "assistant_you" : "assistant_name")}</strong><p>{turn.text}</p></li>)}</ol>
    {busy && <p role="status">{t("assistant_thinking")}</p>}
    {notice && <p role="status">{notice}</p>}
    {error && <><Problem error={error}/><button type="button" disabled={busy} onClick={() => void send(turns.at(-1)?.role === "user" ? turns.at(-1)!.text : draft)}>{t("assistant_retry")}</button></>}
    <form onSubmit={submit}>
      <label htmlFor={`assistant-prompt-${conversationId.current}`}>{t("assistant_input_label")}</label>
      <textarea id={`assistant-prompt-${conversationId.current}`} ref={field} value={draft} maxLength={2000} rows={3} disabled={busy} onChange={(event) => setDraft(event.target.value)} placeholder={t("assistant_placeholder")}/>
      <button type="submit" disabled={busy || draft.trim() === ""}>{t("assistant_send")}</button>
    </form>
  </section>;
}
