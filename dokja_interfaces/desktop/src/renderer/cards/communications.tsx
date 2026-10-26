import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from "react";

import { isRecord, text } from "../../shared/records.js";
import type { CommunicationChannel, CommunicationHistory, CommunicationSend, MemeItem, MemePage, MemePoolRefresh, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import { WorkspacePanel, useWorkspaceRefresh } from "./workspace_panel.js";
import { CardFrame, OffNotice, Problem, describeError } from "./card_frame.js";
import { AttachedMeme, MemePicker } from "./memes.js";
import { parseMemePage } from "./memes_model.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

const unexpected = (): LoadResult<never> => ({ ok: false, error: { code: "unexpected", message: "unexpected communications reply" } });

// The shell and the orchestrator share a key before any message is read or sent; without it they
// answer that access is not configured.
const NOT_SET_UP = "not-set-up";
export const notSetUp = (message: string) => /access is not configured/i.test(message);

export function CommunicationsCard({ transport, active = true }: { transport: Transport; active?: boolean }) {
  const t = useTranslate();
  const load = useCallback(async (): Promise<LoadResult<CommunicationChannel[] | typeof NOT_SET_UP>> => {
    const answer = await transport.request("communications.channels", {}, { timeoutMs: 15_000 });
    // Not set up is an answer, not a failure: say what to do instead of showing the error.
    if (!answer.ok && notSetUp(answer.error.message)) return { ok: true, data: NOT_SET_UP };
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || !Array.isArray(answer.result.channels)) return unexpected();
    return { ok: true, data: answer.result.channels.filter((row): row is CommunicationChannel => isRecord(row) && typeof row.id === "string" && typeof row.name === "string") };
  }, [transport]);
  // read again quietly, keeping what is shown: reloading the list must not throw away the open
  // conversation, which the same refresh brings to its latest messages on its own
  const { state, poll } = useCard(load);
  return <WorkspacePanel trail={[t("nav_messages")]} fill>
    <CardFrame title={t("communications_title")} state={state} onReload={poll}>
      {(channels) => channels === NOT_SET_UP
        ? <div className="note" data-tone="unchecked"><p>{t("communications_not_set_up")}</p><p className="detail">{t("communications_not_set_up_how")}</p></div>
        : channels.length === 0 ? <p>{t("communications_no_channels")}</p> : <ChannelList channels={channels} transport={transport} active={active} />}
    </CardFrame>
  </WorkspacePanel>;
}

// A conversation an interface keeps the text of is told apart by its id, which carries the platform.
const WHATSAPP = "whatsapp:";
const isWhatsApp = (id: string) => id.startsWith(WHATSAPP);

// Laid out like a messenger: the conversations on the left, the one chosen on the right with its
// history and its composer. With a single conversation the rail would be a column for one entry, so
// it becomes a strip above the thread. While a message is on its way the list waits, so a send
// cannot cross to another conversation.
function ChannelList({ channels, transport, active }: { channels: CommunicationChannel[]; transport: Transport; active: boolean }) {
  const t = useTranslate();
  const [selected, setSelected] = useState(channels[0]?.id ?? "");
  const [sending, setSending] = useState(false);
  const chosen = channels.find((channel) => channel.id === selected);
  return <div className="messenger" data-single={channels.length === 1 ? "" : undefined}>
    <nav className="channels" aria-label={t("communications_channel")}>
      <ul>
        {channels.map((channel) => <li key={channel.id}>
          <button type="button" aria-label={channelLabel(channel, t)} aria-current={channel.id === selected ? "true" : undefined} disabled={sending} onClick={() => setSelected(channel.id)}>
            <ChannelName channel={channel} />
          </button>
        </li>)}
      </ul>
    </nav>
    <section className="thread">
      <div className="thread-head">
        <p>{chosen && isWhatsApp(chosen.id) ? t("communications_whatsapp_identity") : t("communications_identity")}</p>
        {chosen && isWhatsApp(chosen.id) && chosen.retentionDays !== undefined && <p>{t("communications_retention", { days: chosen.retentionDays })}</p>}
      </div>
      <Conversation key={selected} channel={selected} transport={transport} active={active} onSending={setSending} />
    </section>
  </div>;
}

// The words that name a conversation: a WhatsApp one by its alias, a Discord one by its channel and,
// when it has a name of its own, the identifier after it.
function channelLabel(channel: CommunicationChannel, t: ReturnType<typeof useTranslate>): string {
  if (isWhatsApp(channel.id)) return t("communications_whatsapp_label", { name: channel.name });
  return channel.name === channel.id ? channel.id : `#${channel.name} · ${channel.id}`;
}

// A conversation in the rail: its name, and under it, dimmer, what identifies it. The separator is
// there for the accessible name and the narrow strip; in the rail the two lines say it already.
function ChannelName({ channel }: { channel: CommunicationChannel }) {
  const t = useTranslate();
  if (isWhatsApp(channel.id) || channel.name === channel.id) return <span className="channel-name">{channelLabel(channel, t)}</span>;
  return <>
    <span className="channel-name">{`#${channel.name}`}</span>
    <span className="channel-sep"> · </span>
    <span className="channel-id">{channel.id}</span>
  </>;
}

// Who wrote a message of a WhatsApp conversation: the interface sends a fixed label, said here in the
// language of the screen. A Discord author is a name and is shown as it is.
function authorLabel(channel: string, author: string, t: ReturnType<typeof useTranslate>): string {
  if (!isWhatsApp(channel)) return author;
  switch (author) {
    case "owner": return t("communications_who_owner");
    case "bot": return t("communications_who_bot");
    case "contact": return t("communications_who_contact");
    case "member": return t("communications_who_member");
    default: return author;
  }
}

function Conversation({ channel, transport, active, onSending }: { channel: string; transport: Transport; active: boolean; onSending: (busy: boolean) => void }) {
  const t = useTranslate();
  const { ago } = useTimes();
  const [cursor, setCursor] = useState("");
  const [page, setPage] = useState<CommunicationHistory>();
  const load = useCallback(async (): Promise<LoadResult<CommunicationHistory>> => {
    const answer = await transport.request("communications.history", { channel_id: channel, before: cursor }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || answer.result.channelId !== channel || !Array.isArray(answer.result.messages) || typeof answer.result.before !== "string") return unexpected();
    return { ok: true, data: answer.result as CommunicationHistory };
  }, [transport, channel, cursor]);
  const { state, reload } = useCard(load);
  useEffect(() => { if (state.phase === "ready") setPage(state.data); }, [state]);
  // Poll only the visible, latest page; a failing request stops automatic refresh until the
  // person retries, avoiding repeated rate-limit or permission failures.
  useEffect(() => {
    if (!active || cursor || state.phase !== "ready") return;
    const timer = setInterval(() => { if (document.visibilityState === "visible") reload(); }, 15_000);
    return () => clearInterval(timer);
  }, [active, cursor, state.phase, reload]);
  // The panel's one refresh brings the conversation back to its latest messages.
  const latest = useCallback(() => { setCursor(""); reload(); }, [reload]);
  useWorkspaceRefresh(latest, state.phase === "loading");
  // After a failed older page the shown page is still the one before it, so its cursor is the one
  // that failed: setting it again changes nothing, and the request has to be asked for.
  const older = () => {
    if (!page?.before) return;
    if (page.before === cursor) reload();
    else setCursor(page.before);
  };
  // The history is read from its end, where the composer is: a new page opens at its last message,
  // and the same page read again follows the end only while the reader is still at it.
  const scroller = useRef<HTMLDivElement>(null);
  const atEnd = useRef(true);
  const shownCursor = useRef("");
  useLayoutEffect(() => {
    const box = scroller.current;
    if (!box || !page) return;
    if (atEnd.current || shownCursor.current !== cursor) box.scrollTop = box.scrollHeight;
    shownCursor.current = cursor;
  }, [page]);
  // The room above the composer changes when a meme is attached or the field grows.
  useEffect(() => {
    const box = scroller.current;
    if (!box || typeof ResizeObserver === "undefined") return;
    const watch = new ResizeObserver(() => { if (atEnd.current) box.scrollTop = box.scrollHeight; });
    watch.observe(box);
    return () => watch.disconnect();
  }, []);
  return <>
    <div className="thread-history" ref={scroller} onScroll={(event) => {
      const box = event.currentTarget;
      atEnd.current = box.scrollHeight - box.scrollTop - box.clientHeight < 48;
    }}>
      <div className="history-top">
        <button type="button" className="quiet" onClick={older} disabled={!page?.before || state.phase === "loading"}>{t("communications_older")}</button>
        {state.phase === "loading" && !page && <p role="status">{t("loading")}</p>}
      </div>
      {state.phase === "error" && <Problem error={state.error} />}
      {page && <>
        {page.messages.length === 0 && <p className="detail">{t("communications_empty")}</p>}
        <ol className="conversation" aria-label={t("communications_history")}>
          {page.messages.map((message) => <li key={message.id} data-bot={message.bot}>
            <p className="message-meta"><strong>{authorLabel(channel, message.author, t)}</strong>{message.bot && !isWhatsApp(channel) && <span className="detail"> · {t("communications_bot")}</span>}
              {" "}<time dateTime={message.timestamp} title={message.timestamp}>{ago(message.timestamp)}</time>
              {message.edited && <span className="detail"> · {t("communications_edited")}</span>}
            </p>
            {message.replyTo && <p className="detail">{t("communications_reply", { id: message.replyTo })}</p>}
            <p className="message-content">{message.content || (message.attachments.length === 0 ? t("communications_no_content") : "")}</p>
            {message.attachments.map((name, index) => <p className="detail" key={index}>{t("communications_attachment", { name })}</p>)}
          </li>)}
        </ol>
      </>}
    </div>
    <Composer channel={channel} transport={transport} onSent={latest} onSending={onSending} />
  </>;
}

// The paperclip stand-in for a meme: a picture frame with a hill and a sun, drawn in the text colour.
function PictureIcon() {
  return <svg className="icon" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true" focusable="false">
    <rect x="1.5" y="2.5" width="13" height="11" fill="none" stroke="currentColor" />
    <circle cx="5.5" cy="6" r="1.25" fill="currentColor" />
    <path d="M2 12.5l4-4 3 3 2-2 3 3" fill="none" stroke="currentColor" />
  </svg>;
}

const MEME_PAGE = 12;

// The message is one row: the field, a small button for a meme, and Send, the only primary. A meme
// is picked from a tray that opens above the row and covers the history instead of pushing it; the
// one picked stays beside the field as a chip, with its one option, until it is sent or taken off.
function Composer({ channel, transport, onSent, onSending }: { channel: string; transport: Transport; onSent: () => void; onSending: (busy: boolean) => void }) {
  const t = useTranslate();
  const id = useId();
  const trayId = useId();
  const [content, setContent] = useState("");
  const [attached, setAttached] = useState<MemeItem>();
  const [forceSend, setForceSend] = useState(false);
  const [memeOffset, setMemeOffset] = useState(0);
  // Memes are something Ningo sends: a tray opened from the composer, read when it opens.
  const [picking, setPicking] = useState(false);
  const toggle = useRef<HTMLButtonElement>(null);
  const busy = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  // A WhatsApp conversation takes text only, so it has no memes to attach.
  const textOnly = isWhatsApp(channel);
  const attachment = attached?.url ?? "";
  const run = useCallback(async (draft: { content: string; attachment: string; force: boolean }): Promise<LoadResult<CommunicationSend | Off>> => {
    if (busy.current) return unexpected();
    busy.current = true;
    onSending(true);
    try {
      const answer = await transport.request("communications.send", { channel_id: channel, content: draft.content, ...(textOnly ? {} : { attachment_url: draft.attachment, skip_hashtag: true, ...(draft.force ? { force_nsfw: true } : {}) }) }, { timeoutMs: 60_000 });
      if (!answer.ok) return answer;
      if (!isRecord(answer.result)) return unexpected();
      if (answer.result.off === true) return { ok: true, data: { off: true, reason: String(answer.result.reason ?? "") } };
      if (typeof answer.result.sent !== "boolean") return unexpected();
      const data = answer.result as CommunicationSend;
      // sent: the draft is done; an open tray stays open, to pick the next one
      if (data.sent && mounted.current) { setContent(""); setAttached(undefined); setForceSend(false); onSent(); }
      return { ok: true, data };
    } finally { busy.current = false; if (mounted.current) onSending(false); }
  }, [transport, channel, textOnly, onSent, onSending]);
  const send = useAction(run);
  const sending = send.state.phase === "loading";
  const loadMemes = useCallback(async (offset: number): Promise<LoadResult<MemePage | Off>> => {
    // The shell caps a page of memes at 24, so ask for what it will give.
    const answer = await transport.request("meme.list", { scope: "unsent", limit: MEME_PAGE, offset }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    const page = parseMemePage(answer.result);
    return page ? { ok: true, data: page } : unexpected();
  }, [transport]);
  const memes = useAction(loadMemes);
  // A meme that went out is no longer waiting: an open tray reads its page again, and goes back a
  // page when the one sent was the last on it.
  const sentNow = send.state.phase === "ready" && !send.state.data.off && send.state.data.sent ? send.state.data : undefined;
  useEffect(() => {
    if (!sentNow || !picking) return;
    const shown = memes.state.phase === "ready" && !memes.state.data.off ? memes.state.data.memes.length : 0;
    const offset = shown <= 1 && memeOffset > 0 ? Math.max(0, memeOffset - MEME_PAGE) : memeOffset;
    setMemeOffset(offset);
    memes.start(offset);
  }, [sentNow]);
  const togglePicker = () => {
    if (!picking) memes.start(memeOffset);
    setPicking((value) => !value);
  };
  const closePicker = () => { setPicking(false); toggle.current?.focus(); };
  const canSend = !sending && (content.trim() !== "" || (attachment !== "" && !textOnly));
  return <form className="message-compose" onSubmit={(event) => {
    event.preventDefault();
    if (canSend) send.start({ content: content.trim(), attachment, force: forceSend && attachment !== "" });
  }}>
    {!textOnly && picking && <div className="meme-tray" id={trayId}>
      <RefreshMemes transport={transport} disabled={sending || memes.state.phase === "loading"} onRefreshed={() => { setMemeOffset(0); memes.start(0); }} />
      {memes.state.phase === "loading" && <p className="meme-tray-note" role="status">{t("loading")}</p>}
      {memes.state.phase === "error" && <div className="meme-tray-note"><Problem error={memes.state.error} /></div>}
      {memes.state.phase === "ready" && memes.state.data.off && <div className="meme-tray-note"><OffNotice reason={memes.state.data.reason} /></div>}
      {memes.state.phase === "ready" && !memes.state.data.off && <MemePicker transport={transport} memes={memes.state.data.memes} page={memes.state.data} selected={attachment} disabled={sending}
        onSelect={setAttached} onPage={(offset) => { setMemeOffset(offset); memes.start(offset); }}
        onPrepareTagged={setContent} onClearPreparedTag={() => setContent("")} onClose={closePicker} />}
    </div>}
    {!textOnly && attached && <AttachedMeme transport={transport} meme={attached} force={forceSend} disabled={sending}
      onForce={setForceSend} onRemove={() => { setAttached(undefined); setForceSend(false); }} />}
    <div className="compose-row">
      <label className="sr-only" htmlFor={id}>{t("communications_message")}</label>
      <textarea id={id} value={content} maxLength={2000} rows={2} disabled={sending} placeholder={t("communications_message")} onChange={(event) => setContent(event.target.value)} />
      {!textOnly && <button type="button" ref={toggle} className="icon-button" aria-label={t("communications_memes")} title={t("communications_memes")}
        aria-expanded={picking} aria-controls={picking ? trayId : undefined} disabled={sending || memes.state.phase === "loading"} onClick={togglePicker}><PictureIcon /></button>}
      <button type="submit" className="primary" disabled={!canSend}>{textOnly ? t("communications_send_whatsapp") : t("communications_send")}</button>
    </div>
    <div className="compose-status" aria-live="polite">
      {sending && <p role="status">{t("communications_sending")}</p>}
      {send.state.phase === "error" && (send.state.error.code === "timeout" ? <p role="alert">{t("communications_uncertain")}</p> : <Problem error={send.state.error} />)}
      {/* "sent" is about the last message: once the next one is being written it is no longer news */}
      {send.state.phase === "ready" && (send.state.data.off ? <OffNotice reason={send.state.data.reason} /> : !(send.state.data.sent && (content !== "" || attached)) && <p data-tone={send.state.data.sent ? "ok" : "problem"}>{send.state.data.sent ? t("communications_sent") : t("communications_not_sent")}</p>)}
    </div>
  </form>;
}

// Asks the sources for new memes now, instead of waiting for the scheduler's next refresh, then shows
// the first page again. It says how many came in; a source that failed is said too, while what the
// others brought is kept.
function RefreshMemes({ transport, disabled, onRefreshed }: { transport: Transport; disabled: boolean; onRefreshed: () => void }) {
  const t = useTranslate();
  const run = useCallback(async (): Promise<LoadResult<MemePoolRefresh | Off>> => {
    const answer = await transport.request("meme.pool.refresh", {}, { timeoutMs: 120_000 });
    if (!answer.ok) return answer;
    const result = answer.result;
    if (!isRecord(result)) return unexpected();
    if (result.off === true) return { ok: true, data: { off: true, reason: text(result.reason) } };
    if (typeof result.added !== "number") return unexpected();
    return { ok: true, data: { added: result.added, sources: Array.isArray(result.sources) ? result.sources.map(text) : [] } };
  }, [transport]);
  const refresh = useAction(run);
  // Once per refresh that came back: the callback is a new function on every render of the tray.
  const refreshed = useRef(onRefreshed);
  refreshed.current = onRefreshed;
  useEffect(() => { if (refresh.state.phase === "ready") refreshed.current(); }, [refresh.state]);
  const busy = refresh.state.phase === "loading";
  return <div className="meme-refresh">
    <button type="button" className="quiet" disabled={disabled || busy} onClick={() => refresh.start(undefined)}>{t("memes_refresh")}</button>
    <span aria-live="polite">
      {busy && <span role="status">{t("memes_refreshing")}</span>}
      {refresh.state.phase === "ready" && ("off" in refresh.state.data
        ? <span data-tone="off">{refresh.state.data.reason || t("service_off")}</span>
        : <span>{t(refresh.state.data.added === 0 ? "memes_refreshed_none" : "memes_refreshed", { count: refresh.state.data.added })}</span>)}
      {refresh.state.phase === "error" && <span role="alert" data-tone="problem">{describeError(refresh.state.error, t)}</span>}
    </span>
  </div>;
}
