import { useCallback, useEffect, useState, type CSSProperties, type FormEvent, type ReactNode } from "react";

import type { DigestItem, DigestPage, DigestSource, DigestStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { CardFrame, OffNotice, OpenSystem, Pending, Problem } from "./card_frame.js";
import {
  MAX_SHOWN,
  SHOW_STEP,
  countsOf,
  followingOf,
  nextStep,
  parseDigestPage,
  parseDigestStatus,
  sourceTone,
  type Following,
} from "./digest_model.js";
import type { Tone } from "./health_model.js";
import { useCard, type LoadResult } from "./use_card.js";
import { Outcome } from "./knowledge_add.js";
import { addToBase } from "./knowledge_add_model.js";
import { Button, VoiceBlock } from "./ui.js";
import { useAction } from "./use_action.js";
import { usePreview } from "./use_preview.js";
import { useDiscuss } from "../discussion_context.js";
import { useWorkspaceRefresh } from "./workspace_panel.js";

const TIMEOUT_MS = 20_000;

const unexpected = (message: string): LoadResult<never> => ({
  ok: false,
  error: { code: "unexpected", message },
});

export async function loadDigestStatus(transport: Transport): Promise<LoadResult<DigestStatus | Off>> {
  const answer = await transport.request("digest.status", {}, { timeoutMs: TIMEOUT_MS });
  if (!answer.ok) return answer;
  const status = parseDigestStatus(answer.result);
  return status ? { ok: true, data: status } : unexpected("unexpected digest reply");
}

// The page is the first `shown` items of the digest: a longer page holds the shorter one.
type ReadingFilters = { source: string; query: string; from: string; to: string };
const EMPTY_FILTERS: ReadingFilters = { source: "", query: "", from: "", to: "" };

export async function loadDigestPage(transport: Transport, shown: number, filters?: ReadingFilters): Promise<LoadResult<DigestPage | Off>> {
  const answer = await transport.request("digest.items", { limit: shown, offset: 0, ...Object.fromEntries(Object.entries(filters ?? {}).filter(([, value]) => value !== "")) }, { timeoutMs: TIMEOUT_MS });
  if (!answer.ok) return answer;
  const page = parseDigestPage(answer.result);
  return page ? { ok: true, data: page } : unexpected("unexpected digest items reply");
}

const STATE_IDS: Record<string, MessageId> = {
  ok: "digest_state_ok",
  ready: "digest_state_ready",
  failed: "digest_state_failed",
  pending: "digest_state_pending",
  disabled: "digest_state_disabled",
  invalid: "digest_state_invalid",
  unknown: "digest_state_unknown",
};

// What to say when there is nothing to read because nothing is being followed: Ningo's line when he is
// reading nothing, then the plain sentence, then (for the operator) what to change, as a line apart.
// A directory that cannot be read is a problem, not a quiet shelf, and takes no voice.
const EXPLANATIONS: Record<Exclude<Following, "following">, { id: MessageId; detail?: MessageId; tone: Tone; voice: boolean }> = {
  "directory-error": { id: "digest_directory_error", tone: "problem", voice: false },
  "not-configured": { id: "digest_not_configured", detail: "digest_not_configured_detail", tone: "unchecked", voice: true },
  "no-sources": { id: "digest_no_sources", tone: "off", voice: true },
  "none-enabled": { id: "digest_none_enabled", detail: "digest_none_enabled_detail", tone: "unchecked", voice: true },
};

export function DigestCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(() => loadDigestStatus(transport), [transport]);
  const { state, reload, poll, refreshing } = useCard(load);

  // The panel's refresh reads the sources quietly: the page of readings and its filters stay.
  return (
    <CardFrame title={t("digest_title")} quietTitle fix={<OpenSystem />} state={state} onReload={reload} onRefresh={poll} refreshing={refreshing}>
      {(status) =>
        status.off ? <><OffNotice reason={status.reason} /><OpenSystem /></> : <DigestBody transport={transport} status={status} />
      }
    </CardFrame>
  );
}

function DigestBody({ transport, status }: { transport: Transport; status: DigestStatus }) {
  const t = useTranslate();
  const following = followingOf(status);
  const explanation = following === "following" ? undefined : EXPLANATIONS[following];

  // What the sources are doing, in one line, and the list of them behind it: open by itself when one
  // of them fails, since that is what explains a reading that does not come.
  const counts = countsOf(status);
  const strip = (
    <>
      <div className="sources-strip">
        {counts.length > 0 && <p>{counts.map(({ state, count }) => t(COUNT_IDS[state], { count })).join(" · ")}</p>}
        {status.sources.length > 0 && (
          <SourceList sources={status.sources} title="digest_sources_title" open={explanation !== undefined || status.failed + status.invalid > 0} />
        )}
      </div>
      {!!status.providers?.length && (
        <SourceList sources={status.providers} title="digest_providers_title" open={status.providers.some((provider) => provider.state === "failed")} processing />
      )}
    </>
  );

  return (
    <>
      {explanation && (
        <div className="digest-explained">
          {explanation.voice && <p className="voice">{t("digest_voice_none")}</p>}
          <p className="note" data-tone={explanation.tone}>
            {t(explanation.id, { message: status.directoryError })}
          </p>
          {explanation.detail && <p className="detail">{t(explanation.detail)}</p>}
          {following === "directory-error" && <OpenSystem />}
        </div>
      )}
      {following === "following" ? <Reading transport={transport} sources={status.sources} strip={strip} /> : strip}
    </>
  );
}

const COUNT_IDS = {
  ok: "digest_count_ok",
  failed: "digest_count_failed",
  pending: "digest_count_pending",
  disabled: "digest_count_disabled",
  invalid: "digest_count_invalid",
} as const satisfies Record<string, MessageId>;

// What the feeds brought: the filters, the strip that says how the sources are doing (given by the card
// around, since it is about the sources and not about the readings), then the readings themselves.
function Reading({ transport, sources, strip }: { transport: Transport; sources: readonly DigestSource[]; strip: ReactNode }) {
  const t = useTranslate();
  const { ago } = useTimes();
  const [shown, setShown] = useState(SHOW_STEP);
  const [draft, setDraft] = useState<ReadingFilters>(EMPTY_FILTERS);
  const [filters, setFilters] = useState<ReadingFilters>(EMPTY_FILTERS);
  const load = useCallback(() => loadDigestPage(transport, shown, filters), [transport, shown, filters]);
  const invalidDates = !!draft.from && !!draft.to && draft.from > draft.to;
  const apply = (event: FormEvent) => {
    event.preventDefault();
    if (invalidDates) return;
    setShown(SHOW_STEP);
    setFilters({ ...draft });
  };
  const reset = () => { setDraft(EMPTY_FILTERS); setFilters(EMPTY_FILTERS); setShown(SHOW_STEP); };
  const anyFilter = Object.values(draft).some(Boolean) || Object.values(filters).some(Boolean);
  // The dates are put away until they are used: they open when the owner opens them or applies some.
  const [datesOpen, setDatesOpen] = useState(false);
  useEffect(() => { if (filters.from || filters.to) setDatesOpen(true); }, [filters.from, filters.to]);

  const { state, reload, poll, refreshing } = useCard(load);
  // The panel's refresh reads this page again where it stands: how many were shown and the filters
  // stay, and the page stays on screen until the new one lands.
  useWorkspaceRefresh(poll, refreshing || state.phase === "loading");

  // Asking for a longer page must not blank the list: the last page stays until the new one lands.
  const [kept, setKept] = useState<DigestPage | undefined>(undefined);
  useEffect(() => {
    if (state.phase === "ready" && !state.data.off) setKept(state.data);
  }, [state]);

  if (state.phase === "ready" && state.data.off) return <><OffNotice reason={state.data.reason} /><OpenSystem /></>;
  const page = state.phase === "ready" ? state.data : kept;
  const loading = state.phase === "loading";

  return (
    <>
      <form className="reading-filters" onSubmit={apply}>
        <label className="reading-search"><span className="sr-only">{t("reading_search")}</span><input type="search" data-library-search="" placeholder={t("reading_search")} value={draft.query} maxLength={100} onChange={event => setDraft({ ...draft, query: event.target.value })} /></label>
        <label>{t("reading_source")}<select value={draft.source} onChange={event => setDraft({ ...draft, source: event.target.value })}>
          <option value="">{t("reading_all_sources")}</option>
          {sources.map(source => <option key={source.id} value={source.id}>{source.name}</option>)}
        </select></label>
        <details className="reading-dates" open={datesOpen} onToggle={event => setDatesOpen(event.currentTarget.open)}>
          <summary>{t("reading_dates")}</summary>
          <label>{t("reading_from")}<input type="date" value={draft.from} max={draft.to || undefined} onChange={event => setDraft({ ...draft, from: event.target.value })} /></label>
          <label>{t("reading_to")}<input type="date" value={draft.to} min={draft.from || undefined} onChange={event => setDraft({ ...draft, to: event.target.value })} /></label>
        </details>
        <div className="reading-filter-actions">
          <button type="submit" disabled={invalidDates}>{t("reading_apply")}</button>
          {anyFilter && <Button variant="quiet" onClick={reset}>{t("reading_clear")}</Button>}
        </div>
        {invalidDates && <p role="alert">{t("reading_invalid_dates")}</p>}
      </form>
      {strip}
      {page && !page.off && <>
        {(filters.source || filters.query || filters.from || filters.to) && <p role="status">{t("reading_matches", { count: page.total })}</p>}
        <DigestList page={page} transport={transport} filtered={!!(filters.source || filters.query || filters.from || filters.to)} />
      </>}
      {loading && <Pending />}
      {state.phase === "error" && (
        <>
          <Problem error={state.error} />
          <div className="problem-actions">
            <Button extra="retry" onClick={reload}>{t("retry")}</Button>
            <OpenSystem />
          </div>
        </>
      )}
      {page && !page.off && page.more > 0 &&
        (shown < MAX_SHOWN ? (
          <button type="button" disabled={loading} onClick={() => setShown((count) => Math.min(count + SHOW_STEP, MAX_SHOWN))}>
            {t("digest_more", { count: nextStep(page) })}
          </button>
        ) : (
          <p className="detail">{t("digest_at_limit", { count: MAX_SHOWN })}</p>
        ))}
      {page && !page.off && page.updated !== "" && (
        <p className="detail">{t("digest_updated", { ago: ago(page.updated) })}</p>
      )}
    </>
  );
}

function DigestList({ page, transport, filtered }: { page: DigestPage; transport: Transport; filtered: boolean }) {
  const t = useTranslate();
  const discuss = useDiscuss();
  const { ago } = useTimes();
  if (page.items.length === 0) {
    // a filter that matches nothing has said so above; with none, the sources work and nothing came
    return filtered ? null : <VoiceBlock voice={t("digest_voice_quiet")} next={t("digest_empty")} />;
  }
  return (
    <ul className="digest">
      {page.items.map((item, index) => {
        const when = item.published === "" ? "" : ago(item.published);
        const meta = [item.source, when].filter((part) => part !== "").join(", ");
        return <ReadingItem key={`${index}-${item.id}`} item={item} meta={meta} transport={transport}
          onDiscuss={() => discuss({ id: item.id, title: item.title, source: item.source, ...(item.prompts ? { prompts: item.prompts } : {}) })} />;
      })}
    </ul>
  );
}

// One reading: what it is, then what can be done with it on one line. Discussing it with Ningo is the
// thing to do (the discussion happens in Talk, the one place with a composer); keeping it in the Library
// is quieter; the preview opens in place. What saving said stays under the reading.
function ReadingItem({ item, meta, transport, onDiscuss }: { item: DigestItem; meta: string; transport: Transport; onDiscuss: () => void }) {
  const t = useTranslate();
  const save = useSaveReading(transport, item.id, item.title);
  return (
    <li>
      <p className="digest-title">{item.title}</p>
      {meta !== "" && <p className="detail">{meta}</p>}
      {item.summary !== "" && <p className="digest-summary">{item.summary}</p>}
      <div className="digest-actions">
        <button type="button" disabled={!item.id} aria-label={t("assistant_continue") + ": " + item.title} onClick={onDiscuss}>
          {t("assistant_continue")}
        </button>
        {item.id && save.button}
        <ReadingPreview item={item} transport={transport} />
      </div>
      {item.id && save.feedback}
    </li>
  );
}

function ReadingPreview({ item, transport }: { item: DigestItem; transport: Transport }) {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  const [attempt, setAttempt] = useState(0);
  return <details className="reading-preview" onToggle={event => setOpen(event.currentTarget.open)}>
    <summary>{t("reading_preview")}</summary>
    {open && <div className="reading-preview-content">
      {item.mediaUrl && <ReadingMedia key={attempt} transport={transport} url={item.mediaUrl} title={item.title} onRetry={() => setAttempt(attempt + 1)} />}
      {(item.previewText || item.summary) ? <>
        {!item.previewText && <p className="detail">{t("reading_excerpt")}</p>}
        <div className="reading-preview-text" tabIndex={0} aria-label={item.title}>{item.previewText || item.summary}</div>
      </> : !item.mediaUrl && <p className="detail">{t("reading_no_preview")}</p>}
    </div>}
  </details>;
}

function ReadingMedia({ transport, url, title, onRetry }: { transport: Transport; url: string; title: string; onRetry: () => void }) {
  const t = useTranslate();
  const preview = usePreview(transport, url, true);
  if (preview.phase === "loading") return <Pending />;
  if (preview.phase === "failed") return <><Problem error={preview.error} /><button type="button" onClick={onRetry}>{t("retry")}</button></>;
  return preview.dataUrl.startsWith("data:video/")
    ? <video src={preview.dataUrl} aria-label={title} controls playsInline preload="metadata" />
    : <img src={preview.dataUrl} alt={title} loading="lazy" />;
}

// The screen names the reading; the shell builds the ingest request from cached source data. The button
// and what it said are apart so the button can sit in the reading's row and the words under the row.
function useSaveReading(transport: Transport, id: string, title: string) {
  const t = useTranslate();
  const run = useCallback(() => addToBase(transport, { mode: "reading", reading: id }), [transport, id]);
  const { state, start } = useAction(run);
  return {
    button: (
      <Button variant="quiet" aria-label={t("reading_save_named", { title })} disabled={state.phase === "loading"} onClick={() => start(undefined)}>
        {t("reading_save")}
      </Button>
    ),
    feedback: (
      <div className="reading-save" aria-live="polite">
        {state.phase === "loading" && <p role="status">{t("reading_saving")}</p>}
        {state.phase === "error" && <Problem error={state.error} />}
        {state.phase === "ready" && <Outcome result={state.data} />}
      </div>
    ),
  };
}

function SourceList({ sources, title, open, processing = false }: { sources: readonly DigestSource[]; title: MessageId; open: boolean; processing?: boolean }) {
  const t = useTranslate();
  const { ago } = useTimes();
  return (
    <details className="sources" open={open}>
      <summary>{t(title)}</summary>
      <ul className="services">
        {sources.map((source, index) => {
          const detail = [
            source.error,
            source.items > 0 ? t("digest_source_items", { count: source.items }) : "",
            source.lastOk !== "" ? t("digest_source_updated", { ago: ago(source.lastOk) }) : "",
          ]
            .filter((part) => part !== "")
            .join(", ");
          return (
            <li key={source.id} data-tone={sourceTone(processing && source.state === "pending" ? "ready" : source.state)} style={{ "--i": index } as CSSProperties}>
              <span className="service-name">{source.name}</span>
              <span className="service-status">{t(STATE_IDS[processing && source.state === "pending" ? "ready" : source.state] ?? "digest_state_unknown")}</span>
              {detail !== "" && <span className="service-detail">{detail}</span>}
            </li>
          );
        })}
      </ul>
    </details>
  );
}
