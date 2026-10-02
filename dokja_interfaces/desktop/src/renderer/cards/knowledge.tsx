import { useCallback, useId, useState } from "react";

import type { KnowledgeSearch, KnowledgeStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice, Problem } from "./card_frame.js";
import {
  parseKnowledgeSearch,
  parseKnowledgeStatus,
  similarityOf,
} from "./knowledge_model.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

// The embedder behind a search can take a few seconds, so a search may wait longer than a status.
const STATUS_TIMEOUT_MS = 15_000;
const SEARCH_TIMEOUT_MS = 30_000;
const RESULTS_WANTED = 5;
const MAX_QUERY_CHARS = 500;

const unexpected = (message: string): LoadResult<never> => ({
  ok: false,
  error: { code: "unexpected", message },
});

export async function loadKnowledge(transport: Transport): Promise<LoadResult<KnowledgeStatus | Off>> {
  const answer = await transport.request("knowledge.status", {}, { timeoutMs: STATUS_TIMEOUT_MS });
  if (!answer.ok) return answer;
  const status = parseKnowledgeStatus(answer.result);
  return status ? { ok: true, data: status } : unexpected("unexpected knowledge reply");
}

export async function searchKnowledge(
  transport: Transport,
  query: string,
): Promise<LoadResult<KnowledgeSearch | Off>> {
  const answer = await transport.request(
    "knowledge.search",
    { query, k: RESULTS_WANTED },
    { timeoutMs: SEARCH_TIMEOUT_MS },
  );
  if (!answer.ok) return answer;
  const search = parseKnowledgeSearch(answer.result);
  return search ? { ok: true, data: search } : unexpected("unexpected search reply");
}

export function KnowledgeCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(() => loadKnowledge(transport), [transport]);
  const { state, reload } = useCard(load);

  return (
    <CardFrame kindLabel={t("kind_knowledge")} title={t("knowledge_title")} state={state} onReload={reload}>
      {(status) =>
        status.off ? (
          <OffNotice reason={status.reason} />
        ) : (
          <>
            <KnowledgeStatusLines status={status} />
            <SearchBox transport={transport} />
          </>
        )
      }
    </CardFrame>
  );
}

function KnowledgeStatusLines({ status }: { status: KnowledgeStatus }) {
  const t = useTranslate();
  const similarity = similarityOf(status);
  return (
    <>
      <p>{t("knowledge_counts", { documents: status.documents, chunks: status.chunks })}</p>
      <p className="note" data-tone={similarity === "on" ? "ok" : "unchecked"}>
        {similarity === "on" && t("knowledge_similarity_on", { model: status.embedModel })}
        {similarity === "off" && t("knowledge_similarity_off")}
        {similarity === "down" && t("knowledge_similarity_down")}
      </p>
      {status.pendingEmbeddings > 0 && (
        <p className="note" data-tone="unchecked">
          {t("knowledge_pending", { count: status.pendingEmbeddings })}
        </p>
      )}
    </>
  );
}

function SearchBox({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const inputId = useId();
  const [query, setQuery] = useState("");
  const run = useCallback((asked: string) => searchKnowledge(transport, asked), [transport]);
  const { state, start } = useAction(run);
  const searching = state.phase === "loading";
  const asked = query.trim();

  return (
    <>
      <form
        className="search"
        onSubmit={(event) => {
          event.preventDefault();
          if (asked !== "" && !searching) start(asked);
        }}
      >
        <label htmlFor={inputId}>{t("knowledge_ask")}</label>
        <input
          id={inputId}
          type="search"
          value={query}
          maxLength={MAX_QUERY_CHARS}
          autoComplete="off"
          onChange={(event) => setQuery(event.target.value)}
        />
        <button type="submit" disabled={searching || asked === ""}>
          {t("knowledge_search")}
        </button>
      </form>
      <div aria-live="polite">
        {searching && <p role="status">{t("knowledge_searching")}</p>}
        {state.phase === "error" && <Problem error={state.error} />}
        {state.phase === "ready" && <SearchResults search={state.data} />}
      </div>
    </>
  );
}

function SearchResults({ search }: { search: KnowledgeSearch | Off }) {
  const t = useTranslate();
  if (search.off) return <OffNotice reason={search.reason} />;
  if (search.hits.length === 0) {
    return (
      <p className="note" data-tone="off">
        {t("knowledge_no_hits")}
      </p>
    );
  }

  return (
    <>
      {search.degraded && (
        <p className="note" data-tone="unchecked">
          {t("knowledge_degraded")}
          {search.reason && <span className="detail"> {search.reason}</span>}
        </p>
      )}
      {search.relevantCount === 0 && (
        <p className="note" data-tone="unchecked">
          {t("knowledge_none_relevant")}
        </p>
      )}
      <ol className="hits">
        {search.hits.map((hit) => (
          <li key={hit.rank} data-relevant={hit.relevant}>
            <p>
              <strong>{hit.title}</strong>
              {hit.heading && <span className="detail"> {hit.heading}</span>}
            </p>
            <p className="hit-text">{hit.text}</p>
            <p className="detail">
              {[
                hit.relevant ? "" : t("knowledge_weak"),
                hit.score === null ? "" : t("knowledge_score", { score: hit.score.toFixed(2) }),
                hit.kind,
                hit.tags.join(", "),
                hit.sourceRef,
              ]
                .filter((part) => part !== "")
                .join(", ")}
            </p>
          </li>
        ))}
      </ol>
    </>
  );
}
