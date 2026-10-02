import { useCallback, useEffect, useRef, useState } from "react";

import type { MemeItem, MemePage, MemeStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice, Pending, Problem } from "./card_frame.js";
import {
  PAGE_SIZE,
  pageRange,
  parseMemePage,
  parseMemeStatus,
  shortDate,
  type Scope,
} from "./memes_model.js";
import { usePreview } from "./use_preview.js";
import { useCard, type LoadResult } from "./use_card.js";

const TIMEOUT_MS = 20_000;

const unexpected = (message: string): LoadResult<never> => ({
  ok: false,
  error: { code: "unexpected", message },
});

export async function loadMemeStatus(transport: Transport): Promise<LoadResult<MemeStatus | Off>> {
  const answer = await transport.request("meme.status", {}, { timeoutMs: TIMEOUT_MS });
  if (!answer.ok) return answer;
  const status = parseMemeStatus(answer.result);
  return status ? { ok: true, data: status } : unexpected("unexpected meme reply");
}

export async function loadMemePage(
  transport: Transport,
  scope: Scope,
  offset: number,
): Promise<LoadResult<MemePage | Off>> {
  const answer = await transport.request(
    "meme.list",
    { scope, limit: PAGE_SIZE, offset },
    { timeoutMs: TIMEOUT_MS },
  );
  if (!answer.ok) return answer;
  const page = parseMemePage(answer.result);
  return page ? { ok: true, data: page } : unexpected("unexpected meme list reply");
}

export function MemesCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(() => loadMemeStatus(transport), [transport]);
  const { state, reload } = useCard(load);

  return (
    <CardFrame kindLabel={t("kind_memes")} title={t("memes_title")} state={state} onReload={reload}>
      {(status) =>
        status.off ? <OffNotice reason={status.reason} /> : <MemeQueue transport={transport} status={status} />
      }
    </CardFrame>
  );
}

function MemeQueue({ transport, status }: { transport: Transport; status: MemeStatus }) {
  const t = useTranslate();
  const [scope, setScope] = useState<Scope>("unsent");
  const [offset, setOffset] = useState(0);
  const load = useCallback(() => loadMemePage(transport, scope, offset), [transport, scope, offset]);
  const { state, reload } = useCard(load);

  const choose = (next: Scope) => {
    setScope(next);
    setOffset(0);
  };

  return (
    <>
      <p>{t("memes_counts", { unsent: status.unsent, sent: status.sent })}</p>
      <div className="scope" role="group">
        {(["unsent", "sent"] as const).map((option) => (
          <button key={option} type="button" aria-pressed={scope === option} onClick={() => choose(option)}>
            {t(option === "unsent" ? "memes_scope_unsent" : "memes_scope_sent")}
          </button>
        ))}
      </div>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && (
        <>
          <Problem error={state.error} />
          <button type="button" onClick={reload}>
            {t("retry")}
          </button>
        </>
      )}
      {state.phase === "ready" &&
        (state.data.off ? (
          <OffNotice reason={state.data.reason} />
        ) : (
          <MemePageView
            transport={transport}
            page={state.data}
            scope={scope}
            onMove={(next) => setOffset(next)}
          />
        ))}
    </>
  );
}

function MemePageView({
  transport,
  page,
  scope,
  onMove,
}: {
  transport: Transport;
  page: MemePage;
  scope: Scope;
  onMove: (offset: number) => void;
}) {
  const t = useTranslate();
  const [opened, setOpened] = useState<number | undefined>(undefined);
  const opener = useRef<HTMLElement | null>(null);

  // Closing hands the focus back to the meme that was opened.
  const close = useCallback(() => {
    setOpened(undefined);
    opener.current?.focus();
  }, []);

  if (page.memes.length === 0) {
    return (
      <p className="note" data-tone="off">
        {t("memes_empty")}
      </p>
    );
  }

  const { from, to } = pageRange(page);
  const meme = opened === undefined ? undefined : page.memes[opened];
  return (
    <>
      <div className="pager">
        <button type="button" disabled={page.offset === 0} onClick={() => onMove(Math.max(page.offset - PAGE_SIZE, 0))}>
          {t("memes_previous")}
        </button>
        <span className="detail">{t("memes_range", { from, to, total: page.total })}</span>
        <button type="button" disabled={to >= page.total} onClick={() => onMove(page.offset + PAGE_SIZE)}>
          {t("memes_next")}
        </button>
      </div>
      <ul className="memes">
        {page.memes.map((item, index) => (
          <li key={`${page.offset}-${index}`}>
            <MemePreview
              transport={transport}
              meme={item}
              onOpen={(button) => {
                opener.current = button;
                setOpened(index);
              }}
            />
            <p className="meme-title">{item.title || t("memes_untitled")}</p>
            <MemeDetails meme={item} scope={scope} />
          </li>
        ))}
      </ul>
      {meme && opened !== undefined && (
        <MemeViewer
          transport={transport}
          memes={page.memes}
          index={opened}
          scope={scope}
          onMove={setOpened}
          onClose={close}
        />
      )}
    </>
  );
}

function MemeDetails({ meme, scope }: { meme: MemeItem; scope: Scope }) {
  const t = useTranslate();
  return (
    <>
      <p className="detail">{[meme.source, meme.tags].filter((part) => part !== "").join(", ")}</p>
      <p className="detail">
        {scope === "sent" && meme.sentAt
          ? t("memes_sent_on", { date: shortDate(meme.sentAt) })
          : t("memes_added", { date: shortDate(meme.createdAt) })}
      </p>
    </>
  );
}

// A meme is a picture or a short video; the shell says which by the type of the data it hands back.
function Media({ dataUrl, label, large }: { dataUrl: string; label: string; large?: boolean }) {
  if (!dataUrl.startsWith("data:video/")) return <img src={dataUrl} alt="" />;
  // In the grid a clip is a silent loop, like a GIF, and the tile opens it. Opened, it plays with
  // its sound and its controls.
  return large ? (
    <video src={dataUrl} aria-label={label} autoPlay loop controls playsInline />
  ) : (
    <video src={dataUrl} aria-label={label} autoPlay muted loop playsInline preload="auto" disablePictureInPicture />
  );
}

function MemePreview({
  transport,
  meme,
  onOpen,
}: {
  transport: Transport;
  meme: MemeItem;
  onOpen: (button: HTMLElement) => void;
}) {
  const t = useTranslate();
  const preview = usePreview(transport, meme.url, meme.url !== "");
  const title = meme.title || t("memes_untitled");

  if (meme.url === "" || preview.phase === "failed") return <div className="preview">{t("memes_no_preview")}</div>;
  if (preview.phase === "loading") return <div className="preview" aria-busy="true" />;
  return (
    <button
      type="button"
      className="preview preview-open"
      aria-label={t("memes_open", { title })}
      onClick={(event) => onOpen(event.currentTarget)}
    >
      <Media dataUrl={preview.dataUrl} label={title} />
    </button>
  );
}

// One meme, large, over the page. Escape or a click outside closes it; the arrows move between the
// memes of the page. The file is already in the preview cache, so it opens without a new request.
function MemeViewer({
  transport,
  memes,
  index,
  scope,
  onMove,
  onClose,
}: {
  transport: Transport;
  memes: MemeItem[];
  index: number;
  scope: Scope;
  onMove: (index: number) => void;
  onClose: () => void;
}) {
  const t = useTranslate();
  const meme = memes[index] as MemeItem;
  const preview = usePreview(transport, meme.url, meme.url !== "");
  const title = meme.title || t("memes_untitled");
  const closeButton = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    closeButton.current?.focus();
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.ctrlKey || event.altKey || event.metaKey) return;
      if (event.key === "Escape") onClose();
      else if (event.key === "ArrowLeft" && index > 0) onMove(index - 1);
      else if (event.key === "ArrowRight" && index < memes.length - 1) onMove(index + 1);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [index, memes.length, onMove, onClose]);

  return (
    <div className="viewer" onClick={onClose}>
      <div role="dialog" aria-modal="true" aria-label={title} className="viewer-panel" onClick={(event) => event.stopPropagation()}>
        <div className="viewer-bar">
          <span className="detail">{t("memes_viewer_position", { current: index + 1, total: memes.length })}</span>
          <button ref={closeButton} type="button" onClick={onClose}>
            {t("memes_close")}
          </button>
        </div>
        <div className="viewer-media">
          {preview.phase === "ready" && <Media key={meme.url} dataUrl={preview.dataUrl} label={title} large />}
          {preview.phase === "loading" && <div aria-busy="true" />}
          {preview.phase === "failed" && <p>{t("memes_no_preview")}</p>}
        </div>
        <p className="meme-title">{title}</p>
        <MemeDetails meme={meme} scope={scope} />
        <div className="pager">
          <button type="button" disabled={index === 0} onClick={() => onMove(index - 1)}>
            {t("memes_viewer_previous")}
          </button>
          <button type="button" disabled={index === memes.length - 1} onClick={() => onMove(index + 1)}>
            {t("memes_viewer_next")}
          </button>
        </div>
      </div>
    </div>
  );
}
