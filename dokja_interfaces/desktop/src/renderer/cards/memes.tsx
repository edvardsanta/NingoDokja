import { useCallback, useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { isRecord, text } from "../../shared/records.js";
import type { MemeItem, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { describeError, needsWrittenText } from "./card_frame.js";
import { usePreview } from "./use_preview.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

// The memes waiting in the queue, as a grid to attach one from when writing a message. A meme is
// not a workspace of its own: it is something Ningo sends, so it is picked where messages are
// written. A click attaches a meme, a click on the attached one takes it off again. The picker has
// three parts: the page of memes with its pager above, the grid, and the tagging row at the foot.

// What the tray says about the selected meme, under Suggestion.
type MemeAdvice = { kind: "looking" | "none" | "unreadable" | "failed" | "off" } | { kind: "found"; hashtag: string };

const ADVICE_TEXT: Record<Exclude<MemeAdvice["kind"], "found">, MessageId> = {
  looking: "memes_suggestion_looking",
  none: "memes_suggestion_none",
  unreadable: "memes_suggestion_unreadable",
  failed: "memes_suggestion_failed",
  off: "memes_suggestion_off",
};

function adviceOf(answer: Awaited<ReturnType<Transport["request"]>>): MemeAdvice {
  if (!answer.ok) return { kind: needsWrittenText(answer.error) ? "unreadable" : "failed" };
  if (!isRecord(answer.result)) return { kind: "failed" };
  if (answer.result.off === true) return { kind: "off" };
  const hashtag = text(answer.result.hashtag).trim();
  return answer.result.relevant === true && hashtag ? { kind: "found", hashtag } : { kind: "none" };
}

export function MemePicker({
  transport,
  memes,
  page,
  selected,
  disabled,
  onSelect,
  onPage,
  onPrepareTagged,
  onClearPreparedTag,
  onClose,
}: {
  transport: Transport;
  memes: readonly MemeItem[];
  page: { total: number; offset: number };
  selected: string;
  disabled: boolean;
  onSelect: (meme: MemeItem | undefined) => void;
  onPage: (offset: number) => void;
  onPrepareTagged: (hashtag: string) => void;
  onClearPreparedTag: () => void;
  // Escape, with nothing larger open over it, closes the picker.
  onClose?: () => void;
}) {
  const t = useTranslate();
  const [hashtag, setHashtag] = useState("");
  // The meme's text, written by the owner when it cannot be read from the image.
  const [written, setWritten] = useState("");
  const textId = useId();
  const [createdTags, setCreatedTags] = useState<string[]>([]);
  const [advice, setAdvice] = useState<MemeAdvice>({ kind: "none" });
  const [viewer, setViewer] = useState<{ dataUrl: string; title: string }>();
  const selectedMeme = memes.find((meme) => meme.url === selected);
  const loadCreatedTags = useCallback(async () => {
    const [knowledge, memory] = await Promise.all([
      transport.request("knowledge.list", { limit: 100, offset: 0 }, { timeoutMs: 15_000 }),
      transport.request("memory.list", { state: "resolved", limit: 100, offset: 0 }, { timeoutMs: 15_000 }),
    ]);
    const knowledgeTags = knowledge.ok && isRecord(knowledge.result) && Array.isArray(knowledge.result.documents)
      ? knowledge.result.documents.filter(isRecord)
      .filter((document) => text(document.kind) === "meme_hashtag")
      .map((document) => text(document.title).trim())
      .filter(Boolean) : [];
    const acceptedSuggestions = memory.ok && isRecord(memory.result) && Array.isArray(memory.result.experiences)
      ? memory.result.experiences.filter(isRecord)
        .filter((experience) => text(experience.action) === "hashtag.suggest" && text(experience.outcome) === "accepted")
        .map((experience) => text(experience.detail).trim())
        .filter(Boolean) : [];
    const tags = [...knowledgeTags, ...acceptedSuggestions]
      .map((tag) => tag.startsWith("#") ? tag : `#${tag}`);
    setCreatedTags(Array.from(new Set(tags)).sort());
  }, [transport]);
  const createTag = useCallback(async (): Promise<LoadResult<{ hashtag: string } | Off>> => {
    if (!selectedMeme || !hashtag.trim()) return { ok: false, error: { code: "invalid", message: "a hashtag is required" } };
    const answer = await transport.request("meme.hashtag.tag", { url: selectedMeme.url, hashtag: hashtag.trim(), ...(written.trim() ? { text: written.trim() } : {}) }, { timeoutMs: 30_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result)) return { ok: false, error: { code: "unexpected", message: "unexpected hashtag reply" } };
    if (answer.result.off === true) return { ok: true, data: { off: true, reason: text(answer.result.reason) } };
    const value = text(answer.result.hashtag);
    if (!value) return { ok: false, error: { code: "unexpected", message: "unexpected hashtag reply" } };
    onPrepareTagged(value);
    setHashtag("");
    setWritten("");
    await loadCreatedTags();
    return { ok: true, data: { hashtag: value } };
  }, [hashtag, written, loadCreatedTags, onPrepareTagged, selectedMeme, transport]);
  const tag = useAction(createTag);
  useEffect(() => {
    setHashtag("");
    setWritten("");
  }, [selected]);
  // What Ningo would tag this meme with, read from the meme itself. Showing it records nothing: a
  // suggestion becomes an experience only when it is used.
  useEffect(() => {
    if (!selected) return;
    let current = true;
    setAdvice({ kind: "looking" });
    void transport.request("meme.hashtag.suggest", { url: selected }, { timeoutMs: 30_000 }).then((answer) => {
      if (current) setAdvice(adviceOf(answer));
    });
    return () => { current = false; };
  }, [selected, transport]);
  const askText = tag.state.phase === "error" && needsWrittenText(tag.state.error) || written !== "";
  // a meme with no address cannot be attached
  const attachable = memes.filter((meme) => meme.url !== "");
  const root = useRef<HTMLDivElement>(null);
  // Opened from the keyboard or the mouse, the picker takes the focus to its first meme.
  useEffect(() => {
    root.current?.querySelector<HTMLElement>("button.meme-pick:not(:disabled)")?.focus();
  }, []);

  useEffect(() => {
    void loadCreatedTags();
  }, [loadCreatedTags]);

  useEffect(() => {
    if (!viewer) return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") setViewer(undefined);
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [viewer]);

  if (attachable.length === 0) {
    return (
      <p className="note" data-tone="off">
        {t("memes_empty")}
      </p>
    );
  }

  const busy = tag.state.phase === "loading";
  const from = page.offset + 1;
  const to = page.offset + memes.length;
  return (
    <div className="meme-picker" ref={root} onKeyDown={(event) => {
      // Escape closes the large view first (its own listener); the typed tag is not thrown away by it
      if (event.key !== "Escape" || viewer || event.target instanceof HTMLInputElement) return;
      onClose?.();
    }}>
      <div className="meme-picker-head">
        <span>{t("memes_range", { from, to, total: Math.max(page.total, to) })}</span>
        <div className="meme-picker-pager">
          <button type="button" className="quiet" aria-label={t("memes_previous")} title={t("memes_previous")}
            disabled={disabled || page.offset === 0} onClick={() => onPage(Math.max(0, page.offset - memes.length))}>
            <span aria-hidden="true">‹</span>
          </button>
          <button type="button" className="quiet" aria-label={t("memes_next")} title={t("memes_next")}
            disabled={disabled || page.offset + memes.length >= page.total || memes.length === 0} onClick={() => onPage(page.offset + memes.length)}>
            <span aria-hidden="true">›</span>
          </button>
        </div>
      </div>
      <ul className="memes" aria-label={t("communications_meme")}>
        {attachable.map((meme) => (
          <li key={meme.url}>
            <MemeTile
              transport={transport}
              meme={meme}
              attached={selected === meme.url}
              disabled={disabled}
              onToggle={() => onSelect(selected === meme.url ? undefined : meme)}
              onOpen={(dataUrl) => setViewer({ dataUrl, title: meme.title || t("memes_untitled") })}
            />
            <p className="meme-title" title={meme.title || t("memes_untitled")}>{meme.title || t("memes_untitled")}</p>
          </li>
        ))}
      </ul>
      {selectedMeme && <div className="meme-tag-form">
        <label>
          <span>{t("memes_tag")}</span>
          <input list="meme-tag-suggestions" value={hashtag} disabled={busy}
            onChange={(event) => {
              const value = event.target.value;
              setHashtag(value);
              if (!value.trim()) onClearPreparedTag();
            }} placeholder="#tag" />
        </label>
        <button type="button" disabled={busy || !hashtag.trim()} onClick={() => tag.start(undefined)}>
          {t("memes_create_tag")}
        </button>
        {busy && <span className="meme-tag-status" role="status">{t("memes_creating_tag")}</span>}
        {tag.state.phase === "ready" && ("off" in tag.state.data
          ? <span className="meme-tag-status" data-tone="off">{tag.state.data.reason}</span>
          : <span className="meme-tag-status" data-tone="ok">{t("memes_tagged_ready", { tag: tag.state.data.hashtag })}</span>)}
        {tag.state.phase === "error" && <span className="meme-tag-status" data-tone="problem" role="alert" title={tag.state.error.message}>{describeError(tag.state.error, t)}</span>}
        {askText && <div className="meme-tag-text">
          <label htmlFor={`${textId}-text`}>{t("memes_tag_text")}</label>
          <textarea id={`${textId}-text`} aria-describedby={`${textId}-hint`} value={written} maxLength={2000} rows={3} disabled={busy}
            onChange={(event) => setWritten(event.target.value)} />
          <p id={`${textId}-hint`} className="detail">{t("memes_tag_text_hint")}</p>
        </div>}
        <div className="meme-tag-suggestions" role="status" aria-label={t("memes_tag_suggestion")}>
          <span>{t("memes_tag_suggestion")}</span>
          {advice.kind === "found"
            ? <button type="button" className="meme-tag-advice" disabled={busy}
                onClick={() => { setHashtag(advice.hashtag); onPrepareTagged(advice.hashtag); }}>{advice.hashtag}</button>
            : <span className="detail">{t(ADVICE_TEXT[advice.kind])}</span>}
        </div>
        {createdTags.length > 0 && <div className="meme-tag-suggestions" aria-label={t("memes_tags_known")}>
          <span>{t("memes_tags_known")}</span>
          {createdTags.map((known) => <button key={known} type="button" className="quiet"
            onClick={() => { setHashtag(known); onPrepareTagged(known); }}
            disabled={busy}>{known}</button>)}
        </div>}
      </div>}
      <datalist id="meme-tag-suggestions">
        {createdTags.map((known) => <option key={known} value={known} />)}
      </datalist>
      {/* on the page itself, so it covers the window rather than the panel */}
      {viewer && createPortal(<div className="media-viewer-backdrop" role="presentation" onMouseDown={(event) => {
        if (event.target === event.currentTarget) setViewer(undefined);
      }}>
        <section className="media-viewer" role="dialog" aria-modal="true" aria-label={viewer.title}>
          <button type="button" className="media-viewer-close" onClick={() => setViewer(undefined)}>{t("memes_close_preview")}</button>
          <Media dataUrl={viewer.dataUrl} label={viewer.title} expanded />
        </section>
      </div>, document.body)}
    </div>
  );
}

// The meme about to go out with the message, shown where it is written: the picture small, its name,
// the one option that is about it (sending past the media screening) and a way to take it off. It
// stays when the picker is closed, so the message always says what it carries.
export function AttachedMeme({
  transport,
  meme,
  force,
  disabled,
  onForce,
  onRemove,
}: {
  transport: Transport;
  meme: MemeItem;
  force: boolean;
  disabled: boolean;
  onForce: (force: boolean) => void;
  onRemove: () => void;
}) {
  const t = useTranslate();
  const preview = usePreview(transport, meme.url, true);
  const title = meme.title || t("memes_untitled");
  return <div className="attached-meme">
    <span className="attached-thumb" aria-hidden="true">
      {preview.phase === "ready" && <Media dataUrl={preview.dataUrl} label={title} />}
    </span>
    <span className="attached-title" title={title}>{title}</span>
    <label className="attached-force">
      <input type="checkbox" checked={force} disabled={disabled} onChange={(event) => onForce(event.target.checked)} />
      {t("communications_force_send")}
    </label>
    <button type="button" className="quiet attached-remove" aria-label={t("memes_detach", { title })} title={t("memes_detach", { title })}
      disabled={disabled} onClick={onRemove}>
      <span aria-hidden="true">×</span>
    </button>
  </div>;
}

// A meme is a picture or a short video; the shell says which by the type of the data it hands back.
// In the grid a clip is a silent loop, like a GIF.
function Media({ dataUrl, label, expanded = false }: { dataUrl: string; label: string; expanded?: boolean }) {
  if (!dataUrl.startsWith("data:video/")) {
    return <img src={dataUrl} alt="" />;
  }
  return <video className={expanded ? "media-expanded" : undefined} src={dataUrl} aria-label={label}
    autoPlay={expanded ? undefined : true} muted={!expanded} loop={!expanded} playsInline controls preload="auto"
    onClick={(event) => event.stopPropagation()} />;
}

// A meme in the grid: the picture itself attaches it (and a second press takes it off), a small
// corner button shows it large. The attached one is lit and says so.
function MemeTile({
  transport,
  meme,
  attached,
  disabled,
  onToggle,
  onOpen,
}: {
  transport: Transport;
  meme: MemeItem;
  attached: boolean;
  disabled: boolean;
  onToggle: () => void;
  onOpen: (dataUrl: string) => void;
}) {
  const t = useTranslate();
  const preview = usePreview(transport, meme.url, true);
  const title = meme.title || t("memes_untitled");
  return <div className="meme-tile" data-attached={attached ? "" : undefined}>
    <button type="button" className="preview meme-pick" aria-label={t("memes_attach", { title })}
      aria-pressed={attached} aria-busy={preview.phase === "loading"} disabled={disabled} onClick={onToggle}>
      {preview.phase === "ready" && <Media dataUrl={preview.dataUrl} label={title} />}
      {preview.phase === "failed" && <span>{t("memes_no_preview")}</span>}
    </button>
    {attached && <span className="meme-badge" aria-hidden="true">{t("memes_attached")}</span>}
    {preview.phase === "ready" && <button type="button" className="meme-zoom" aria-label={t("memes_view_named", { title })}
      title={t("memes_view_named", { title })} disabled={disabled} onClick={() => onOpen(preview.dataUrl)}>
      <span aria-hidden="true">⤢</span>
    </button>}
  </div>;
}
