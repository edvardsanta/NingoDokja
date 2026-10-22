import { useState } from "react";
import { isRecord, text } from "../../shared/records.js";
import type { MemeItem, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { usePreview } from "./use_preview.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

// The memes waiting in the queue, as a grid to attach one from when writing a message. A meme is
// not a workspace of its own: it is something Ningo sends, so it is picked where messages are
// written. A click attaches a meme, a click on the attached one takes it off again.
export function MemePicker({
  transport,
  memes,
  page,
  selected,
  disabled,
  onSelect,
  onPage,
}: {
  transport: Transport;
  memes: readonly MemeItem[];
  page: { total: number; offset: number };
  selected: string;
  disabled: boolean;
  onSelect: (url: string) => void;
  onPage: (offset: number) => void;
}) {
  const t = useTranslate();
  const [hashtag, setHashtag] = useState("");
  const selectedMeme = memes.find((meme) => meme.url === selected);
  const tag = useAction(async (): Promise<LoadResult<{ hashtag: string } | Off>> => {
    if (!selectedMeme || !hashtag.trim()) return { ok: false, error: { code: "invalid", message: "a hashtag is required" } };
    const answer = await transport.request("meme.hashtag.tag", { url: selectedMeme.url, hashtag: hashtag.trim() }, { timeoutMs: 30_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result)) return { ok: false, error: { code: "unexpected", message: "unexpected hashtag reply" } };
    if (answer.result.off === true) return { ok: true, data: { off: true, reason: text(answer.result.reason) } };
    const value = text(answer.result.hashtag);
    return value ? { ok: true, data: { hashtag: value } } : { ok: false, error: { code: "unexpected", message: "unexpected hashtag reply" } };
  });
  // a meme with no address cannot be attached
  const attachable = memes.filter((meme) => meme.url !== "");

  if (attachable.length === 0) {
    return (
      <p className="note" data-tone="off">
        {t("memes_empty")}
      </p>
    );
  }

  return (
    <>
    <ul className="memes" aria-label={t("communications_meme")}>
      {attachable.map((meme) => (
        <li key={meme.url}>
          <MemeTile
            transport={transport}
            meme={meme}
            attached={selected === meme.url}
            disabled={disabled}
            onToggle={() => onSelect(selected === meme.url ? "" : meme.url)}
          />
          <p className="meme-title">{meme.title || t("memes_untitled")}</p>
        </li>
      ))}
    </ul>
    <div className="pager">
      <button type="button" disabled={disabled || page.offset === 0} onClick={() => onPage(Math.max(0, page.offset - memes.length))}>
        {t("memes_previous")}
      </button>
      <button type="button" disabled={disabled || page.offset + memes.length >= page.total || memes.length === 0} onClick={() => onPage(page.offset + memes.length)}>
        {t("memes_next")}
      </button>
    </div>
    {selectedMeme && <form className="pager" onSubmit={(event) => { event.preventDefault(); if (tag.state.phase !== "loading") tag.start(undefined); }}>
      <input aria-label={t("memes_tag")} value={hashtag} disabled={tag.state.phase === "loading"} onChange={(event) => setHashtag(event.target.value)} placeholder="#tag" />
      <button type="submit" disabled={tag.state.phase === "loading" || !hashtag.trim()}>{t("memes_create_tag")}</button>
      {tag.state.phase === "ready" && ("off" in tag.state.data ? <span>{tag.state.data.reason}</span> : <span>{t("memes_tagged", { tag: tag.state.data.hashtag })}</span>)}
      {tag.state.phase === "error" && <span role="alert">{tag.state.error.message}</span>}
    </form>}
    </>
  );
}

// A meme is a picture or a short video; the shell says which by the type of the data it hands back.
// In the grid a clip is a silent loop, like a GIF.
function Media({ dataUrl, label }: { dataUrl: string; label: string }) {
  if (!dataUrl.startsWith("data:video/")) return <img src={dataUrl} alt="" />;
  return <video src={dataUrl} aria-label={label} autoPlay muted loop playsInline controls preload="auto" onClick={(event) => event.stopPropagation()} />;
}

function MemeTile({
  transport,
  meme,
  attached,
  disabled,
  onToggle,
}: {
  transport: Transport;
  meme: MemeItem;
  attached: boolean;
  disabled: boolean;
  onToggle: () => void;
}) {
  const t = useTranslate();
  const preview = usePreview(transport, meme.url, true);
  const title = meme.title || t("memes_untitled");
  return (
    <button
      type="button"
      className="preview preview-open"
      aria-label={t("memes_attach", { title })}
      aria-pressed={attached}
      aria-busy={preview.phase === "loading"}
      disabled={disabled}
      onClick={onToggle}
    >
      {preview.phase === "ready" && <Media dataUrl={preview.dataUrl} label={title} />}
      {preview.phase === "failed" && <span>{t("memes_no_preview")}</span>}
    </button>
  );
}
