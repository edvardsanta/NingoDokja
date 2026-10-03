import { useCallback, useEffect, useId, useRef, useState } from "react";

import { isRecord } from "../../shared/records.js";
import type { CommunicationChannel, CommunicationHistory, CommunicationSend, MemePage, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice, Problem } from "./card_frame.js";
import { parseMemePage } from "./memes_model.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

const unexpected = (): LoadResult<never> => ({ ok: false, error: { code: "unexpected", message: "unexpected communications reply" } });

export function CommunicationsCard({ transport, active = true }: { transport: Transport; active?: boolean }) {
  const t = useTranslate();
  const load = useCallback(async (): Promise<LoadResult<CommunicationChannel[]>> => {
    const answer = await transport.request("communications.channels", {}, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || !Array.isArray(answer.result.channels)) return unexpected();
    return { ok: true, data: answer.result.channels.filter((row): row is CommunicationChannel => isRecord(row) && typeof row.id === "string" && typeof row.name === "string") };
  }, [transport]);
  const { state, reload } = useCard(load);
  return <CardFrame kindLabel={t("kind_communications")} title={t("communications_title")} state={state} onReload={reload}>
    {(channels) => channels.length === 0 ? <p>{t("communications_no_channels")}</p> : <ChannelList channels={channels} transport={transport} active={active} />}
  </CardFrame>;
}

function ChannelList({ channels, transport, active }: { channels: CommunicationChannel[]; transport: Transport; active: boolean }) {
  const t = useTranslate();
  const id = useId();
  const [selected, setSelected] = useState(channels[0]?.id ?? "");
  const [sending, setSending] = useState(false);
  return <>
    <p className="note">{t("communications_identity")}</p>
    <label htmlFor={id}>{t("communications_channel")}</label>{" "}
    <select id={id} value={selected} disabled={sending} onChange={(event) => setSelected(event.target.value)}>
      {channels.map((channel) => <option key={channel.id} value={channel.id}>{channel.name === channel.id ? channel.id : `#${channel.name} · ${channel.id}`}</option>)}
    </select>
    <Conversation key={selected} channel={selected} transport={transport} active={active} onSending={setSending} />
  </>;
}

function Conversation({ channel, transport, active, onSending }: { channel: string; transport: Transport; active: boolean; onSending: (busy: boolean) => void }) {
  const t = useTranslate();
  const { ago } = useTimes();
  const [cursor, setCursor] = useState("");
  const [page, setPage] = useState<CommunicationHistory>();
  const [auto, setAuto] = useState(true);
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
    if (!active || !auto || cursor || state.phase !== "ready") return;
    const timer = setInterval(() => { if (document.visibilityState === "visible") reload(); }, 15_000);
    return () => clearInterval(timer);
  }, [active, auto, cursor, state.phase, reload]);
  const latest = useCallback(() => { setCursor(""); reload(); }, [reload]);
  // After a failed older page the shown page is still the one before it, so its cursor is the one
  // that failed: setting it again changes nothing, and the request has to be asked for.
  const older = () => {
    if (!page?.before) return;
    if (page.before === cursor) reload();
    else setCursor(page.before);
  };
  return <>
    <div className="pager">
      <button type="button" onClick={latest} disabled={state.phase === "loading"}>{t("communications_latest")}</button>
      <button type="button" onClick={older} disabled={!page?.before || state.phase === "loading"}>{t("communications_older")}</button>
      <label><input type="checkbox" checked={auto} onChange={(event) => setAuto(event.target.checked)} /> {t("communications_auto")}</label>
    </div>
    {state.phase === "loading" && <p role="status">{t("loading")}</p>}
    {state.phase === "error" && <Problem error={state.error} />}
    {page && <>
      {page.messages.length === 0 && <p>{t("communications_empty")}</p>}
      <ol className="conversation" aria-label={t("communications_history")}>
        {page.messages.map((message) => <li key={message.id} data-bot={message.bot}>
          <p><strong>{message.author}</strong>{message.bot && <span className="detail"> · {t("communications_bot")}</span>}
            {" "}<time dateTime={message.timestamp} title={message.timestamp}>{ago(message.timestamp)}</time>
            {message.edited && <span className="detail"> · {t("communications_edited")}</span>}
          </p>
          {message.replyTo && <p className="detail">{t("communications_reply", { id: message.replyTo })}</p>}
          <p className="message-content">{message.content || (message.attachments.length === 0 ? t("communications_no_content") : "")}</p>
          {message.attachments.map((name, index) => <p className="detail" key={index}>{t("communications_attachment", { name })}</p>)}
        </li>)}
      </ol>
    </>}
    <Composer channel={channel} transport={transport} onSent={latest} onSending={onSending} />
  </>;
}

function Composer({ channel, transport, onSent, onSending }: { channel: string; transport: Transport; onSent: () => void; onSending: (busy: boolean) => void }) {
  const t = useTranslate();
  const id = useId();
  const [content, setContent] = useState("");
  const [attachment, setAttachment] = useState("");
  const busy = useRef(false);
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const run = useCallback(async (draft: { content: string; attachment: string }): Promise<LoadResult<CommunicationSend | Off>> => {
    if (busy.current) return unexpected();
    busy.current = true;
    onSending(true);
    try {
      const answer = await transport.request("communications.send", { channel_id: channel, content: draft.content, attachment_url: draft.attachment }, { timeoutMs: 60_000 });
      if (!answer.ok) return answer;
      if (!isRecord(answer.result)) return unexpected();
      if (answer.result.off === true) return { ok: true, data: { off: true, reason: String(answer.result.reason ?? "") } };
      if (typeof answer.result.sent !== "boolean") return unexpected();
      const data = answer.result as CommunicationSend;
      if (data.sent && mounted.current) { setContent(""); setAttachment(""); onSent(); }
      return { ok: true, data };
    } finally { busy.current = false; if (mounted.current) onSending(false); }
  }, [transport, channel, onSent, onSending]);
  const send = useAction(run);
  const sending = send.state.phase === "loading";
  const loadMemes = useCallback(async (): Promise<LoadResult<MemePage | Off>> => {
    // The shell caps a page of memes at 24, so ask for what it will give.
    const answer = await transport.request("meme.list", { scope: "unsent", limit: 24, offset: 0 }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    const page = parseMemePage(answer.result);
    return page ? { ok: true, data: page } : unexpected();
  }, [transport]);
  const memes = useAction(loadMemes);
  return <form className="message-compose" onSubmit={(event) => {
    event.preventDefault();
    if (!sending && (content.trim() || attachment)) send.start({ content: content.trim(), attachment });
  }}>
    <label htmlFor={id}>{t("communications_message")}</label>
    <textarea id={id} value={content} maxLength={2000} rows={4} disabled={sending} onChange={(event) => setContent(event.target.value)} />
    <div className="pager">
      <button type="button" disabled={sending || memes.state.phase === "loading"} onClick={() => memes.start(undefined)}>{t("communications_memes")}</button>
      {memes.state.phase === "ready" && !memes.state.data.off && <select aria-label={t("communications_meme")} value={attachment} disabled={sending} onChange={(event) => setAttachment(event.target.value)}>
        <option value="">{t("communications_no_meme")}</option>
        {memes.state.data.memes.map((meme) => <option key={meme.url} value={meme.url}>{meme.title || meme.url}</option>)}
      </select>}
      <button type="submit" disabled={sending || (!content.trim() && !attachment)}>{t("communications_send")}</button>
    </div>
    {memes.state.phase === "error" && <Problem error={memes.state.error} />}
    {memes.state.phase === "ready" && memes.state.data.off && <OffNotice reason={memes.state.data.reason} />}
    <div aria-live="polite">
      {sending && <p role="status">{t("communications_sending")}</p>}
      {send.state.phase === "error" && (send.state.error.code === "timeout" ? <p role="alert">{t("communications_uncertain")}</p> : <Problem error={send.state.error} />)}
      {send.state.phase === "ready" && (send.state.data.off ? <OffNotice reason={send.state.data.reason} /> : <p>{send.state.data.sent ? t("communications_sent") : t("communications_not_sent")}</p>)}
    </div>
  </form>;
}
