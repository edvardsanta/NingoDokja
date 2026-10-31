import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from "react";

import type { KnowledgeHit, KnowledgeSearch, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTranslate } from "../i18n/context.js";
import { OffNotice, OpenSystem, Problem } from "./card_frame.js";
import { parseKnowledgeSearch } from "./knowledge_model.js";
import { Button, VoiceBlock } from "./ui.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

// The embedder behind a search can take a few seconds, so a search may wait longer than a status.
const SEARCH_TIMEOUT_MS = 30_000;
const RESULTS_WANTED = 5;
const MAX_QUERY_CHARS = 500;

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
  return search
    ? { ok: true, data: search }
    : { ok: false, error: { code: "unexpected", message: "unexpected search reply" } };
}

// The search is a mode of the Documents view: what it finds takes the place of the list, under the
// same line, until the owner clears it. It lives in the body, not in its form, because the field sits
// in the toolbar and the hits in the list's place. Typing a new question does not touch the hits on
// screen until it is run; the ones shown stay while the next is on its way.
export function useKnowledgeSearch(transport: Transport) {
  const [query, setQuery] = useState("");
  // what the hits on screen answer, which is not what is typed now
  const [asked, setAsked] = useState("");
  const [kept, setKept] = useState<KnowledgeSearch | Off>();
  const field = useRef<HTMLInputElement>(null);
  const run = useCallback((question: string) => searchKnowledge(transport, question), [transport]);
  const { state, start, reset } = useAction(run);
  useEffect(() => {
    if (state.phase === "ready") setKept(state.data);
    else if (state.phase === "error") setKept(undefined);
  }, [state]);

  const ask = (question: string) => {
    setAsked(question);
    start(question);
  };
  const clear = () => {
    reset();
    setKept(undefined);
    setQuery("");
    setAsked("");
    field.current?.focus();
  };
  const searching = state.phase === "loading";
  // the hits that stand in the list's place: the answer, or the last one while the next is awaited
  const shown = state.phase === "ready" ? state.data : searching ? kept : undefined;
  const failed = state.phase === "error" ? state.error : undefined;
  return { query, setQuery, asked, field, state, ask, clear, searching, shown, failed, active: shown !== undefined || failed !== undefined };
}
export type KnowledgeSearchState = ReturnType<typeof useKnowledgeSearch>;

export function SearchForm({ search }: { search: KnowledgeSearchState }) {
  const t = useTranslate();
  const inputId = useId();
  const { query, setQuery, searching, ask, clear, field } = search;
  const question = query.trim();

  // Esc leaves the search: the field is emptied and the list is back, where the owner typed
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Escape" || (query === "" && !search.active)) return;
    event.preventDefault();
    clear();
  };

  return (
    <form
      className="search"
      onSubmit={(event) => {
        event.preventDefault();
        if (question !== "" && !searching) ask(question);
      }}
    >
      <label htmlFor={inputId} className="sr-only">{t("knowledge_ask")}</label>
      <input
        id={inputId}
        ref={field}
        type="search"
        data-library-search=""
        placeholder={t("knowledge_ask")}
        value={query}
        maxLength={MAX_QUERY_CHARS}
        autoComplete="off"
        onChange={(event) => setQuery(event.target.value)}
        onKeyDown={onKeyDown}
      />
      <button type="submit" disabled={searching || question === ""}>
        {t("knowledge_search")}
      </button>
    </form>
  );
}

// Under the field, always there so a screen reader hears it change: that a search is on its way, or
// what it found, with the way back to the list.
export function SearchState({ search }: { search: KnowledgeSearchState }) {
  const t = useTranslate();
  const { searching, shown, asked, active, clear } = search;
  const found = shown && !shown.off && shown.hits.length > 0 ? shown.hits.length : 0;
  return (
    <div className="results-head" data-active={searching || active ? "" : undefined}>
      <div aria-live="polite">
        {searching && <p role="status">{t("knowledge_searching")}</p>}
        {!searching && found > 0 && <p>{t("knowledge_results", { count: found, query: asked })}</p>}
      </div>
      {active && <Button variant="quiet" size="compact" onClick={clear}>{t("knowledge_clear")}</Button>}
    </div>
  );
}

// What the search found, in the list's place. A failure says so with the way to ask again.
export function SearchResults({ search, onOpen }: { search: KnowledgeSearchState; onOpen: (hit: KnowledgeHit) => void }) {
  const t = useTranslate();
  const { shown, failed, asked, ask } = search;
  if (failed) {
    return (
      <>
        <Problem error={failed} />
        <div className="problem-actions">
          <Button extra="retry" onClick={() => ask(asked)}>{t("retry")}</Button>
        </div>
      </>
    );
  }
  if (!shown) return null;
  if (shown.off) return <><OffNotice reason={shown.reason} /><OpenSystem /></>;
  if (shown.hits.length === 0) return <VoiceBlock voice={t("knowledge_voice_no_hits")} next={t("knowledge_no_hits")} />;
  return <Hits search={shown} onOpen={onOpen} />;
}

function Hits({ search, onOpen }: { search: KnowledgeSearch; onOpen: (hit: KnowledgeHit) => void }) {
  const t = useTranslate();
  const { fixed } = useNumbers();
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
              {/* the title opens the document the passage is from; a hit that names none is only read */}
              {hit.sourceId ? (
                <button type="button" className="row-title" aria-label={t("documents_open_named", { title: hit.title || hit.sourceId })} onClick={() => onOpen(hit)}>
                  <strong>{hit.title}</strong>
                </button>
              ) : (
                <strong>{hit.title}</strong>
              )}
              {hit.heading && <span className="detail"> {hit.heading}</span>}
            </p>
            <p className="hit-text">{hit.text}</p>
            <p className="detail">
              {[
                hit.relevant ? "" : t("knowledge_weak"),
                hit.score === null ? "" : t("knowledge_score", { score: fixed(hit.score, 2) }),
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
