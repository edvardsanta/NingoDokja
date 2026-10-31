import { useCallback, useRef } from "react";

import { isRecord, text } from "../../shared/records.js";
import type { KnowledgeStatus, Off, ReindexResult } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { OffNotice, Problem } from "./card_frame.js";
import { similarityOf } from "./knowledge_model.js";
import { Button } from "./ui.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

const count = (value: unknown): number =>
  typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : 0;
const unexpected = (): LoadResult<never> => ({ ok: false, error: { code: "unexpected", message: "unexpected knowledge reply" } });

// The one line under the search: what the Library holds, how well search works, what is not indexed
// and, at its end, the filter that shows what Ningo keeps for himself. Similarity that works is a
// quiet chip; only a search that cannot use it says so at length, since that changes what it finds.
// `facts` is false where the counts would say nothing (a Library with nothing brought in yet).
export function KnowledgeLine({ transport, status, facts, internal, onInternal, hidden, onChanged }: {
  transport: Transport;
  status?: KnowledgeStatus;
  facts: boolean;
  internal: boolean;
  onInternal?: (internal: boolean) => void;
  // How many documents the counts include that the list leaves out, while the filter is off.
  hidden: number;
  onChanged: () => void;
}) {
  const t = useTranslate();
  const similarity = status ? similarityOf(status) : undefined;
  return (
    <>
      <div className="library-line">
        {facts && status && (
          <>
            <span>{t("knowledge_counts", { documents: status.documents, chunks: status.chunks })}</span>
            {similarity === "on" ? (
              <span className="chip" data-tone="ok" title={t("knowledge_similarity_on", { model: status.embedModel })}>
                {t("knowledge_similarity_chip")}
              </span>
            ) : (
              <span className="note" data-tone="unchecked">
                {similarity === "off" ? t("knowledge_similarity_off") : t("knowledge_similarity_down")}
              </span>
            )}
            {status.pendingEmbeddings > 0 && (
              <span className="chip" data-tone="unchecked">
                {t("knowledge_pending", { count: status.pendingEmbeddings })}
              </span>
            )}
            <IndexPending transport={transport} pending={status.pendingEmbeddings} onChanged={onChanged} />
          </>
        )}
        {onInternal && (
          <button type="button" className="chip-toggle" aria-pressed={internal} onClick={() => onInternal(!internal)}>
            {internal && <span aria-hidden="true">✓ </span>}
            {t("documents_show_internal")}
          </button>
        )}
      </div>
      {hidden > 0 && <p className="detail library-hidden">{t("documents_hidden", { count: hidden })}</p>}
    </>
  );
}

// Indexes one batch of the passages that wait for an embedding, from the line that says they wait.
export function IndexPending({ transport, pending, onChanged }: {
  transport: Transport; pending: number; onChanged: () => void;
}) {
  const t = useTranslate();
  const busy = useRef(false);
  const run = useCallback(async (): Promise<LoadResult<ReindexResult | Off>> => {
    if (busy.current) return unexpected();
    busy.current = true;
    try {
      const answer = await transport.request("knowledge.reindex", { limit: 32 }, { timeoutMs: 120_000 });
      if (!answer.ok) return answer;
      const result = answer.result;
      if (!isRecord(result)) return unexpected();
      if (result.off === true) return { ok: true, data: { off: true, reason: text(result.reason) } };
      if (typeof result.embedded !== "number" || typeof result.remaining !== "number") return unexpected();
      onChanged();
      return { ok: true, data: { embedded: count(result.embedded), remaining: count(result.remaining), degraded: result.degraded === true, reason: text(result.reason) } };
    } finally {
      busy.current = false;
    }
  }, [transport, onChanged]);
  const { state, start } = useAction(run);
  return <>
    {/* the name stays what it says it does; the words on it are the short ones */}
    {pending > 0 && <Button size="compact" aria-label={t("index_title")} disabled={state.phase === "loading"} onClick={() => start(undefined)}>{t("index_now")}</Button>}
    <div className="line-feedback" aria-live="polite">
      {state.phase === "loading" && <p role="status">{t("index_running")}</p>}
      {state.phase === "error" && (state.error.code === "timeout"
        ? <p role="alert">{t("index_timeout")}</p> : <Problem error={state.error} />)}
      {state.phase === "ready" && (state.data.off ? <OffNotice reason={state.data.reason} /> : <>
        <p>{t("index_result", { count: state.data.embedded, remaining: state.data.remaining })}</p>
        {state.data.degraded && <p className="note" data-tone="unchecked">{t("index_degraded")} {state.data.reason}</p>}
      </>)}
    </div>
  </>;
}
