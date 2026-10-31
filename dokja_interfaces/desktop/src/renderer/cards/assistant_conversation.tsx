import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import type { Transport } from "../../shared/transport.js";
import type { ArchivedConversation, ArchivedTurn, ArchivedTurnState } from "../../shared/replies.js";
import { isRecord } from "../../shared/records.js";
import type { MessageId, Translate } from "../i18n/i18n.js";
import type { TransportError } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { Reading } from "../discussion_context.js";
import { Problem } from "./card_frame.js";
import { latestConversation } from "./conversation_list.js";
import { Button, Notice } from "./ui.js";

// A send as the screen shows it, named by its send id. The archive's turn is the truth whenever the
// screen has it; "unknown" is a send whose outcome the screen could not read back.
export type Turn = {
  sendId: string;
  id: string;
  // its place in the conversation, once the archive has it
  seq?: number;
  content: string;
  reply: string;
  state: ArchivedTurnState | "unknown";
  reason: string;
  // what the runtime reported answering, which may differ from what is selected now
  answeredBy?: string;
};

export type Conversation = {
  // the archive's id once the conversation has been started there, empty before the first message
  id: string;
  turns: readonly Turn[];
  // the reply written so far while it is on its way, when the shell can stream it; never the reply
  preview: string;
  // the orchestrator has this send waiting behind earlier turns of the conversation
  queued: boolean;
  // the archive holds turns before the first one on screen
  older: boolean;
  loadingOlder: boolean;
  olderError?: TransportError;
  readOlder: () => Promise<void>;
  draft: string;
  setDraft: (draft: string) => void;
  busy: boolean;
  error: TransportError | undefined;
  // the archive could not start the conversation, so the last message was given back unsent
  unkept: boolean;
  // the archive's conversation this one continues, when it was reopened or restored
  opened: ArchivedConversation | undefined;
  // a conversation reopened from the archive: its transcript is being read, or could not be
  loading: boolean;
  loadError: TransportError | undefined;
  reload: () => void;
  field: RefObject<HTMLTextAreaElement | null>;
  send: (content: string) => Promise<void>;
  retry: () => void;
  // The latest send is still on its way, here or from another interface, and can be asked to stop.
  stoppable: boolean;
  stopping: boolean;
  stopFailed: boolean;
  stop: () => Promise<void>;
};

const fromArchive = (turn: ArchivedTurn): Turn => ({
  sendId: turn.sendId, id: turn.id, seq: turn.seq, content: turn.content, reply: turn.reply, state: turn.state, reason: turn.reason,
  ...(turn.model ? { answeredBy: turn.provider ? `${turn.provider} / ${turn.model}` : turn.model } : {}),
});

// How often, and for how long, a reopened conversation looks again at a send still on its way: its
// reply was being written for a screen that is gone, so nothing else will bring it here.
const PENDING_RECHECK_MS = 3_000;
const PENDING_RECHECKS = 40;
// How near the end of the conversation still counts as being at the end.
const FOLLOW_SLACK_PX = 80;
// A new conversation is named by the start of its first message.
const TITLE_CHARS = 60;

function titleFrom(message: string): string {
  const line = message.replace(/\s+/g, " ").trim();
  return line.length > TITLE_CHARS ? `${line.slice(0, TITLE_CHARS - 1)}…` : line;
}

// The reasons the archive records in its own words are said in the screen's language; any other
// reason (a provider's, a reading's) is shown as it was recorded.
const KNOWN_REASONS: Readonly<Record<string, MessageId>> = {
  "the request stopped while waiting for the earlier reply": "assistant_reason_stopped_waiting",
  "the request stopped before the reply": "assistant_reason_stopped",
  "the owner stopped this reply": "assistant_reason_owner_stopped",
  "the model returned no reply": "assistant_reason_no_reply",
  "the conversation archive could not be read": "assistant_reason_archive",
};

function reasonText(reason: string, t: Translate): string {
  const known = KNOWN_REASONS[reason];
  return known ? t(known) : reason || t("assistant_no_reply_unknown");
}

function newSendId(): string {
  return globalThis.crypto?.randomUUID?.() ?? `send-${Date.now()}-${Math.random().toString(36).slice(2, 12)}`;
}

// What a send still needs: a turn that ended without a reply is retried by the owner, a send whose
// outcome is unknown is sent again under its own send id (the archive answers it once), and one still
// on its way is only read again.
// Reading the latest page again keeps what was read before it, and the sends the archive does not
// hold yet stay last.
export function mergedTurns(previous: readonly Turn[], archived: Turn[]): Turn[] {
  const first = archived[0]?.seq ?? Number.POSITIVE_INFINITY;
  const earlier = previous.filter((turn) => turn.seq !== undefined && turn.seq < first);
  const unheld = previous.filter((turn) => !earlier.includes(turn) && !archived.some((each) => each.sendId === turn.sendId));
  return [...earlier, ...archived, ...unheld];
}

export function retryable(turn: Turn | undefined): boolean {
  return turn !== undefined && turn.state !== "completed";
}

// One conversation with Ningo, kept by the orchestrator's archive: it is started there with the first
// message, every message carries its own send id, and after a failure the screen reads the archive
// back instead of guessing. A remount (a new conversation) drops whatever was on its way.
// restore asks it to continue the latest conversation the archive holds, when there is one.
export function useConversation(transport: Transport, reading?: Reading, opened?: ArchivedConversation, restore = false): Conversation {
  const id = useRef(opened?.id ?? "");
  const [conversationId, setConversationId] = useState(opened?.id ?? "");
  const [held, setHeld] = useState(opened);
  const [loading, setLoading] = useState(opened !== undefined || restore);
  const [loadError, setLoadError] = useState<TransportError>();
  const rechecks = useRef(0);
  // A panel that was replaced (another conversation, a new one) reads nothing more.
  const live = useRef(true);
  useEffect(() => () => { live.current = false; }, []);
  const [draft, setDraft] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<TransportError>();
  const [unkept, setUnkept] = useState(false);
  const [preview, setPreview] = useState("");
  const [queued, setQueued] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [stopFailed, setStopFailed] = useState(false);
  // A send this panel is waiting on ends in an error once stopped; the turn says what happened.
  const stopped = useRef(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [olderError, setOlderError] = useState<TransportError>();
  const field = useRef<HTMLTextAreaElement>(null);
  // Only this conversation's previews, and only while its turn is on its way.
  useEffect(() => {
    if (!busy || !transport.onChatPreview) return undefined;
    return transport.onChatPreview((each) => {
      if (!id.current || each.conversationId !== id.current) return;
      if (each.queued === undefined) setPreview(each.text); else setQueued(each.queued);
    });
  }, [busy, transport]);

  const settle = (sendId: string, change: (turn: Turn) => Turn) =>
    setTurns((previous) => previous.map((turn) => (turn.sendId === sendId ? change(turn) : turn)));

  // The archive's turns replace the screen's; a send it does not hold yet stays as the screen knew it.
  // A send that could not be read back is unknown; a send already known pending stays pending.
  const readBack = async (sendId?: string) => {
    if (!live.current) return false;
    const read = await transport.request("conversation.read", { conversation_id: id.current });
    if (!read.ok || !isRecord(read.result) || !Array.isArray(read.result.turns)) {
      if (sendId) settle(sendId, (turn) => (turn.state === "pending" ? { ...turn, state: "unknown" } : turn));
      return false;
    }
    const archived = (read.result.turns as ArchivedTurn[]).map(fromArchive);
    setTurns((previous) => mergedTurns(previous, archived));
    return true;
  };

  const firstSeq = turns[0]?.seq;
  const readOlder = async () => {
    if (firstSeq === undefined || firstSeq <= 1 || loadingOlder) return;
    setLoadingOlder(true); setOlderError(undefined);
    const read = await transport.request("conversation.read", { conversation_id: id.current, before_seq: firstSeq });
    if (!live.current) return;
    setLoadingOlder(false);
    if (read.ok && isRecord(read.result) && Array.isArray(read.result.turns)) {
      const earlier = (read.result.turns as ArchivedTurn[]).map(fromArchive);
      setTurns((previous) => [...earlier.filter((turn) => !previous.some((each) => each.sendId === turn.sendId)), ...previous]);
    } else if (!read.ok) setOlderError(read.error);
  };

  // A reopened or restored conversation is read from the archive before it can be continued. Without
  // one to restore, the conversation is a new one.
  const load = async () => {
    let target = opened ?? held;
    setLoading(true); setLoadError(undefined);
    if (!target && restore) {
      target = await latestConversation(transport);
      if (target && !id.current) {
        id.current = target.id;
        setConversationId(target.id);
        setHeld(target);
      } else target = undefined;
    }
    if (!target) { setLoading(false); return; }
    const read = await transport.request("conversation.read", { conversation_id: target.id }).catch(() => undefined);
    if (read?.ok && isRecord(read.result) && Array.isArray(read.result.turns)) setTurns((read.result.turns as ArchivedTurn[]).map(fromArchive));
    else setLoadError(read && !read.ok ? read.error : { code: "unavailable", message: "The conversation could not be read." });
    setLoading(false);
  };
  useEffect(() => { void load(); }, []);

  // Coming back to the window, the conversation is read again: it may have been continued elsewhere.
  const idle = !busy && !loading;
  useEffect(() => {
    if (!idle) return undefined;
    const again = () => { if (id.current && document.visibilityState !== "hidden") void readBack(); };
    window.addEventListener("focus", again);
    document.addEventListener("visibilitychange", again);
    return () => { window.removeEventListener("focus", again); document.removeEventListener("visibilitychange", again); };
  }, [idle]);

  // The latest send still on its way, with nothing here waiting for it, is looked at again for a while.
  const latestPending = !busy && !loading && turns.at(-1)?.state === "pending";
  useEffect(() => {
    if (!latestPending || rechecks.current >= PENDING_RECHECKS) return undefined;
    const timer = setTimeout(() => { rechecks.current += 1; void readBack(); }, PENDING_RECHECK_MS);
    return () => clearTimeout(timer);
  }, [latestPending, turns]);

  const dispatch = async (sendId: string, payload: Record<string, unknown>) => {
    setBusy(true); setError(undefined); setStopFailed(false);
    try {
      const answer = await transport.request("message.created", { conversation_id: id.current, ...payload }, { timeoutMs: 120_000 });
      const turn = answer.ok && isRecord(answer.result) && isRecord(answer.result.turn) ? (answer.result.turn as unknown as ArchivedTurn) : undefined;
      if (turn) {
        settle(sendId, () => fromArchive(turn));
        // Another interface (the phone) may have added turns meanwhile; the archive's order is kept.
        await readBack();
        return;
      }
      if (answer.ok || !stopped.current) setError(answer.ok ? { code: "unexpected", message: "The assistant returned no reply." } : answer.error);
      await readBack(sendId);
    } catch {
      setError({ code: "unavailable", message: "The assistant could not be reached." });
      await readBack(sendId).catch(() => settle(sendId, (turn) => ({ ...turn, state: "unknown" })));
    } finally { stopped.current = false; setBusy(false); setPreview(""); setQueued(false); field.current?.focus(); }
  };

  // Stopping only asks: the turn, read from the archive, says how the send ended. A send nothing here
  // waits on is read back at once, and a stop that came too late needs no apology once it has settled.
  const stop = async () => {
    const latest = turns.at(-1);
    if (stopping || latest?.state !== "pending" || !id.current) return;
    setStopping(true); setStopFailed(false);
    const waited = busy;
    stopped.current = waited;
    const answer = await transport.request("conversation.stop", { conversation_id: id.current, send_id: latest.sendId }).catch(() => undefined);
    if (!live.current) return;
    setStopping(false);
    if (!answer?.ok) { stopped.current = false; setStopFailed(true); }
    if (!waited) {
      await readBack();
      field.current?.focus();
    }
  };

  const send = async (content: string) => {
    const message = content.trim();
    if (!message || busy || loading || loadError) return;
    setDraft(""); setError(undefined); setUnkept(false);
    if (!id.current) {
      setBusy(true);
      const started = await transport.request("conversation.start", { ...(reading ? { reading_id: reading.id } : {}), title: titleFrom(message) }).catch(() => undefined);
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
    if (latest.state === "pending") { rechecks.current = 0; void readBack(); return; }
    settle(latest.sendId, (turn) => ({ ...turn, state: "pending", reason: "" }));
    if ((latest.state === "failed" || latest.state === "interrupted") && latest.id) void dispatch(latest.sendId, { retry_turn_id: latest.id });
    else void dispatch(latest.sendId, { content: latest.content, send_id: latest.sendId });
  };
  const older = firstSeq !== undefined && firstSeq > 1;
  return { id: conversationId, turns, preview, queued, older, loadingOlder, olderError, readOlder, draft, setDraft, busy, error, unkept, loading, loadError, opened: held, reload: () => void load(), field, send, retry, stoppable: turns.at(-1)?.state === "pending", stopping, stopFailed, stop };
}

// What was said, newest at the bottom, and what is happening now. The end is kept in view as the
// conversation grows. A send without a reply says why, and only the latest can be sent again.
export function Transcript({ conversation }: { conversation: Conversation }) {
  const t = useTranslate();
  const { turns, busy, error, preview } = conversation;
  const end = useRef<HTMLDivElement>(null);
  // The end is followed while the owner is at it. Reading further up stops that until they come back
  // down or send something; a reply being written never takes the page from under them.
  const following = useRef(true);
  useEffect(() => {
    const body = end.current?.closest(".wp-body");
    if (!body) return undefined;
    const read = () => { following.current = body.scrollHeight - body.scrollTop - body.clientHeight <= FOLLOW_SLACK_PX; };
    body.addEventListener("scroll", read, { passive: true });
    return () => body.removeEventListener("scroll", read);
  }, [conversation.loading, conversation.loadError]);
  useEffect(() => { if (busy) following.current = true; }, [busy]);
  useEffect(() => {
    if (following.current) end.current?.scrollIntoView?.({ block: "end" });
  }, [turns.length, busy, error, preview, conversation.loading]);
  // Turns added above keep what is on screen where it was.
  const heightBefore = useRef(0);
  const firstTurn = turns[0]?.sendId;
  useLayoutEffect(() => {
    const body = end.current?.closest(".wp-body");
    if (body && heightBefore.current > 0) body.scrollTop += body.scrollHeight - heightBefore.current;
    heightBefore.current = 0;
  }, [firstTurn]);
  const readOlder = () => {
    following.current = false;
    heightBefore.current = end.current?.closest(".wp-body")?.scrollHeight ?? 0;
    void conversation.readOlder();
  };
  const latest = turns.at(-1);
  if (conversation.loading) return <Notice announce="polite">{t("assistant_opening")}</Notice>;
  if (conversation.loadError) return <><Problem error={conversation.loadError}/><Button size="compact" onClick={conversation.reload}>{t("assistant_reload")}</Button></>;
  return <>
    {conversation.older && <Button size="compact" disabled={conversation.loadingOlder} onClick={readOlder}>{t("assistant_older")}</Button>}
    {conversation.olderError && <Problem error={conversation.olderError}/>}
    <ol className="assistant-turns" aria-live="polite">{turns.flatMap((turn) => [
      <li key={`${turn.sendId}-user`} data-role="user"><strong>{t("assistant_you")}</strong><p>{turn.content}</p></li>,
      ...(turn.state === "completed" ? [<li key={`${turn.sendId}-reply`} data-role="assistant"><strong>{t("assistant_name")}</strong><p>{turn.reply}</p>{turn.answeredBy && <small className="assistant-by">{t("assistant_answered_by", { model: turn.answeredBy })}</small>}</li>] : []),
      ...(turn !== latest && turn.state !== "completed" ? [<li key={`${turn.sendId}-none`} className="detail" data-role="assistant">{t("assistant_no_reply", { reason: reasonText(turn.reason, t) })}</li>] : []),
    ])}</ol>
    {/* Outside the live list, so a reader of the screen hears the reply once, when it lands. */}
    {busy && preview && <ol className="assistant-turns"><li className="assistant-preview" data-role="assistant" aria-busy="true"><strong>{t("assistant_name")}</strong><p>{preview}</p></li></ol>}
    {busy && !preview && <Notice announce="polite">{t(conversation.queued ? "assistant_queued" : "assistant_thinking")}</Notice>}
    {!busy && latest?.state === "pending" && <Notice announce="polite">{t("assistant_still_pending")}</Notice>}
    {conversation.stopFailed && latest?.state === "pending" && <Notice announce="assertive">{t("assistant_stop_failed")}</Notice>}
    {!busy && latest && (latest.state === "failed" || latest.state === "interrupted") && !error && <Notice announce="assertive">{t(latest.state === "interrupted" ? "assistant_interrupted" : "assistant_no_reply", { reason: reasonText(latest.reason, t) })}</Notice>}
    {conversation.unkept && <Notice announce="assertive">{t("assistant_history_unavailable")}</Notice>}
    {error && <Problem error={error}/>}
    {!busy && retryable(latest) && <Button size="compact" onClick={conversation.retry}>{t(latest?.state === "pending" ? "assistant_check_again" : "assistant_retry")}</Button>}
    <div ref={end} />
  </>;
}

// Where the owner writes. Enter sends and Shift+Enter breaks the line. The field is one line until
// what is written needs more; footer shares the quiet row under it with the hint.
export function Composer({ conversation, placeholder, hint, keyshortcuts, footer }: { conversation: Conversation; placeholder: string; hint?: string; keyshortcuts?: string; footer?: ReactNode }) {
  const t = useTranslate();
  const { id, draft, setDraft, field, send } = conversation;
  // The field stays usable while a conversation is read, so focus and a draft are kept; only sending waits.
  const busy = conversation.busy || conversation.loadError !== undefined;
  const waiting = busy || conversation.loading;
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
    <textarea id={fieldId} ref={field} value={draft} aria-keyshortcuts={keyshortcuts} maxLength={2000} rows={1} disabled={busy} aria-describedby={hint ? hintId : undefined}
      onChange={(event) => setDraft(event.target.value)} onKeyDown={onKeyDown} placeholder={placeholder}/>
    <Button type="submit" variant="primary" aria-busy={conversation.busy ? "true" : undefined} disabled={waiting || draft.trim() === ""}>{t("assistant_send")}</Button>
    {(hint || footer || conversation.stoppable) && <div className="composer-foot">
      {hint && <p className="detail composer-hint" id={hintId}>{hint}</p>}
      {footer}
      {conversation.stoppable && <Button size="compact" disabled={conversation.stopping} onClick={() => void conversation.stop()}>{t("assistant_stop")}</Button>}
    </div>}
  </form>;
}

// A conversation about one reading, inline under it: the reading's own suggestions send at once.
// The questions a reading's discussion can open with; each is sent as it is.
export const READING_PROMPTS: readonly MessageId[] = ["assistant_prompt_summary", "assistant_prompt_question", "assistant_prompt_critique"];
