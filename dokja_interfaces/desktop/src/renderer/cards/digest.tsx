import { useCallback, useEffect, useState, type CSSProperties } from "react";

import type { DigestPage, DigestSource, DigestStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { CardFrame, OffNotice, Pending, Problem } from "./card_frame.js";
import {
  MAX_SHOWN,
  SHOW_STEP,
  followingOf,
  nextStep,
  parseDigestPage,
  parseDigestStatus,
  sourceTone,
  type Following,
} from "./digest_model.js";
import type { Tone } from "./health_model.js";
import { useCard, type LoadResult } from "./use_card.js";
import { Outcome, addToBase } from "./knowledge_add.js";
import { useAction } from "./use_action.js";
import { useDiscuss } from "../discussion_context.js";

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
export async function loadDigestPage(transport: Transport, shown: number): Promise<LoadResult<DigestPage | Off>> {
  const answer = await transport.request("digest.items", { limit: shown, offset: 0 }, { timeoutMs: TIMEOUT_MS });
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

// What to say when there is nothing to read because nothing is being followed.
const EXPLANATIONS: Record<Exclude<Following, "following">, { id: MessageId; tone: Tone }> = {
  "directory-error": { id: "digest_directory_error", tone: "problem" },
  "not-configured": { id: "digest_not_configured", tone: "unchecked" },
  "no-sources": { id: "digest_no_sources", tone: "off" },
  "none-enabled": { id: "digest_none_enabled", tone: "unchecked" },
};

export function DigestCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(() => loadDigestStatus(transport), [transport]);
  const { state, reload } = useCard(load);

  return (
    <CardFrame title={t("digest_title")} state={state} onReload={reload}>
      {(status) =>
        status.off ? <OffNotice reason={status.reason} /> : <DigestBody transport={transport} status={status} />
      }
    </CardFrame>
  );
}

function DigestBody({ transport, status }: { transport: Transport; status: DigestStatus }) {
  const t = useTranslate();
  const following = followingOf(status);
  const explanation = following === "following" ? undefined : EXPLANATIONS[following];

  return (
    <>
      {explanation ? (
        <p className="note" data-tone={explanation.tone}>
          {t(explanation.id, { message: status.directoryError })}
        </p>
      ) : (
        <p>
          {t("digest_sources_line", {
            ok: status.ok,
            failed: status.failed,
            pending: status.pending,
            disabled: status.disabled,
            invalid: status.invalid,
          })}
        </p>
      )}
      {following === "following" && <Reading transport={transport} />}
      {!!status.providers?.length && <SourceList sources={status.providers} title="digest_providers_title" open={status.providers.some(provider => provider.state === "failed")} processing />}
      {status.sources.length > 0 && (
        <SourceList sources={status.sources} title="digest_sources_title" open={explanation !== undefined || status.failed + status.invalid > 0} />
      )}
    </>
  );
}

function Reading({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const { ago } = useTimes();
  const [shown, setShown] = useState(SHOW_STEP);
  const load = useCallback(() => loadDigestPage(transport, shown), [transport, shown]);
  const { state, reload } = useCard(load);

  // Asking for a longer page must not blank the list: the last page stays until the new one lands.
  const [kept, setKept] = useState<DigestPage | undefined>(undefined);
  useEffect(() => {
    if (state.phase === "ready" && !state.data.off) setKept(state.data);
  }, [state]);

  if (state.phase === "ready" && state.data.off) return <OffNotice reason={state.data.reason} />;
  const page = state.phase === "ready" ? state.data : kept;
  const loading = state.phase === "loading";

  return (
    <>
      {page && !page.off && <DigestList page={page} transport={transport} />}
      {loading && <Pending />}
      {state.phase === "error" && (
        <>
          <Problem error={state.error} />
          <button type="button" onClick={reload}>
            {t("retry")}
          </button>
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

function DigestList({ page, transport }: { page: DigestPage; transport: Transport }) {
  const t = useTranslate();
  const discuss = useDiscuss();
  const { ago } = useTimes();
  if (page.items.length === 0) {
    return (
      <p className="note" data-tone="off">
        {t("digest_empty")}
      </p>
    );
  }
  return (
    <ul className="digest">
      {page.items.map((item, index) => {
        const when = item.published === "" ? "" : ago(item.published);
        const meta = [item.source, when].filter((part) => part !== "").join(", ");
        return (
          <li key={`${index}-${item.id}`}>
            <p className="digest-title">{item.title}</p>
            {meta !== "" && <p className="detail">{meta}</p>}
            {item.summary !== "" && <p className="digest-summary">{item.summary}</p>}
            {/* The discussion happens in Talk, the one place with a composer. */}
            <button type="button" disabled={!item.id} aria-label={t("assistant_continue") + ": " + item.title}
              onClick={() => discuss({ id: item.id, title: item.title, source: item.source, ...(item.prompts ? { prompts: item.prompts } : {}) })}>
              {t("assistant_continue")}
            </button>
            {item.id && <SaveReading transport={transport} id={item.id} title={item.title} />}
          </li>
        );
      })}
    </ul>
  );
}

// What the world brought in becomes knowledge on a press: the reading's page is added to the
// Library, read by the knowledge service from its address. The screen names the reading by id; the
// shell holds the address. Adding it again refreshes that page.
function SaveReading({ transport, id, title }: { transport: Transport; id: string; title: string }) {
  const t = useTranslate();
  const run = useCallback(() => addToBase(transport, { mode: "reading", reading: id }), [transport, id]);
  const { state, start } = useAction(run);
  return (
    <div className="reading-save">
      <button type="button" aria-label={t("reading_save_named", { title })} disabled={state.phase === "loading"} onClick={() => start(undefined)}>
        {t("reading_save")}
      </button>
      {state.phase === "loading" && <p role="status">{t("reading_saving")}</p>}
      {state.phase === "error" && <Problem error={state.error} />}
      {state.phase === "ready" && <Outcome result={state.data} />}
    </div>
  );
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
