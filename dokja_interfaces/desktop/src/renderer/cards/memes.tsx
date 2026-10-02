import { useCallback, useState } from "react";

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
  if (page.memes.length === 0) {
    return (
      <p className="note" data-tone="off">
        {t("memes_empty")}
      </p>
    );
  }

  const { from, to } = pageRange(page);
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
        {page.memes.map((meme, index) => (
          <li key={`${page.offset}-${index}`}>
            <MemePreview transport={transport} meme={meme} />
            <p className="meme-title">{meme.title || t("memes_untitled")}</p>
            <p className="detail">{[meme.source, meme.tags].filter((part) => part !== "").join(", ")}</p>
            <p className="detail">
              {scope === "sent" && meme.sentAt
                ? t("memes_sent_on", { date: shortDate(meme.sentAt) })
                : t("memes_added", { date: shortDate(meme.createdAt) })}
            </p>
          </li>
        ))}
      </ul>
    </>
  );
}

// A meme is a picture or a short video; the shell says which by the type of the data it hands back.
function MemePreview({ transport, meme }: { transport: Transport; meme: MemeItem }) {
  const t = useTranslate();
  const preview = usePreview(transport, meme.url, meme.url !== "");

  if (meme.url === "" || preview.phase === "failed") return <div className="preview">{t("memes_no_preview")}</div>;
  if (preview.phase === "loading") return <div className="preview" aria-busy="true" />;
  if (preview.dataUrl.startsWith("data:video/")) {
    // A clip plays by itself, silent and in a loop like a GIF, and can be paused or unmuted.
    return (
      <div className="preview">
        <video
          src={preview.dataUrl}
          aria-label={meme.title || t("memes_untitled")}
          autoPlay
          muted
          loop
          controls
          playsInline
          preload="auto"
          disablePictureInPicture
        />
      </div>
    );
  }
  return (
    <div className="preview">
      <img src={preview.dataUrl} alt="" />
    </div>
  );
}
