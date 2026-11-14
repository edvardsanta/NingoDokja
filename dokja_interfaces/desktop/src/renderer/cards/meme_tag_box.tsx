import { useCallback, useId, useState } from "react";

import { isRecord, text } from "../../shared/records.js";
import type { Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { describeError, needsWrittenText } from "./card_frame.js";
import { Button, Notice } from "./ui.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

// Tagging one meme from the readings, where the plugins that feed Ningo bring it: the same action as
// the meme tray in Messages, so one meme has one tag whichever way it was reached. The tag is kept in
// the Library, keyed by the meme's address; tagging again replaces it.
export function MemeTagBox({ transport, url, title, known, onTagged }: {
  transport: Transport;
  url: string;
  title: string;
  // the tag this meme already has in the Library, when it has one
  known?: string;
  onTagged: (tag: string) => void;
}) {
  const t = useTranslate();
  const [hashtag, setHashtag] = useState("");
  // The meme's text, written by the owner when it cannot be read from the image.
  const [written, setWritten] = useState("");
  const [advice, setAdvice] = useState("");
  const textId = useId();
  const create = useCallback(async (): Promise<LoadResult<{ hashtag: string } | Off>> => {
    if (!hashtag.trim()) return { ok: false, error: { code: "invalid", message: "a hashtag is required" } };
    const answer = await transport.request("meme.hashtag.tag", { url, hashtag: hashtag.trim(), ...(written.trim() ? { text: written.trim() } : {}) }, { timeoutMs: 30_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result)) return { ok: false, error: { code: "unexpected", message: "unexpected hashtag reply" } };
    if (answer.result.off === true) return { ok: true, data: { off: true, reason: text(answer.result.reason) } };
    const value = text(answer.result.hashtag);
    if (!value) return { ok: false, error: { code: "unexpected", message: "unexpected hashtag reply" } };
    onTagged(value);
    setHashtag("");
    setWritten("");
    setAdvice("");
    return { ok: true, data: { hashtag: value } };
  }, [hashtag, written, transport, url, onTagged]);
  const tag = useAction(create);
  const busy = tag.state.phase === "loading";
  const askText = (tag.state.phase === "error" && needsWrittenText(tag.state.error)) || written !== "";

  // What Ningo would tag it with, asked only when the owner wants it: reading a meme costs a moment.
  const suggest = useAction(useCallback(async (): Promise<LoadResult<{ hashtag: string }>> => {
    const answer = await transport.request("meme.hashtag.suggest", { url }, { timeoutMs: 30_000 });
    if (!answer.ok) return answer;
    const value = isRecord(answer.result) && answer.result.relevant === true ? text(answer.result.hashtag).trim() : "";
    if (value) setHashtag(value);
    setAdvice(value ? "" : "none");
    return { ok: true, data: { hashtag: value } };
  }, [transport, url]));

  return (
    <div className="meme-tag-form reading-meme-tag">
      {known && <p className="detail reading-meme-known">{t("reading_meme_in_library", { tag: known })}</p>}
      <label>
        <span>{t("memes_tag")}</span>
        <input value={hashtag} disabled={busy} placeholder="#tag" aria-label={`${t("memes_tag")}: ${title}`}
          onChange={(event) => setHashtag(event.target.value)} />
      </label>
      <Button disabled={busy || !hashtag.trim()} aria-label={`${t("memes_create_tag")}: ${title}`} onClick={() => tag.start(undefined)}>
        {t("memes_create_tag")}
      </Button>
      <Button variant="quiet" disabled={busy || suggest.state.phase === "loading"} aria-label={`${t("reading_meme_suggest")}: ${title}`} onClick={() => suggest.start(undefined)}>
        {suggest.state.phase === "loading" ? t("memes_suggestion_looking") : t("reading_meme_suggest")}
      </Button>
      {advice === "none" && <span className="detail">{t("memes_suggestion_none")}</span>}
      {suggest.state.phase === "error" && <span className="detail">{t(needsWrittenText(suggest.state.error) ? "memes_suggestion_unreadable" : "memes_suggestion_failed")}</span>}
      {busy && <Notice as="span" announce="polite" extra="meme-tag-status">{t("memes_creating_tag")}</Notice>}
      {tag.state.phase === "ready" && ("off" in tag.state.data
        ? <span className="meme-tag-status" data-tone="off">{tag.state.data.reason}</span>
        : <span className="meme-tag-status" data-tone="ok">{t("reading_meme_tagged", { tag: tag.state.data.hashtag })}</span>)}
      {tag.state.phase === "error" && <span className="meme-tag-status" data-tone="problem" role="alert" title={tag.state.error.message}>{describeError(tag.state.error, t)}</span>}
      {askText && <div className="meme-tag-text">
        <label htmlFor={`${textId}-text`}>{t("memes_tag_text")}</label>
        <textarea id={`${textId}-text`} aria-describedby={`${textId}-hint`} value={written} maxLength={2000} rows={3} disabled={busy}
          onChange={(event) => setWritten(event.target.value)} />
        <p id={`${textId}-hint`} className="detail">{t("memes_tag_text_hint")}</p>
      </div>}
    </div>
  );
}
