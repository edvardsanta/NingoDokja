import { useCallback, useContext, useId, useState } from "react";

import type { KnowledgeHit, KnowledgeSearch, KnowledgeStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice, OpenSystem, Problem } from "./card_frame.js";
import { KnowledgeDocumentsPanel } from "./knowledge_documents.js";
import { LibraryContext, StandaloneLibrary, useLibrary } from "./library_context.js";
import { parseKnowledgeSearch } from "./knowledge_model.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

// The embedder behind a search can take a few seconds, so a search may wait longer than a status.
const SEARCH_TIMEOUT_MS = 30_000;
const RESULTS_WANTED = 5;
const MAX_QUERY_CHARS = 500;

const unexpected = (message: string): LoadResult<never> => ({
  ok: false,
  error: { code: "unexpected", message },
});

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

// The Documents view of the Library: finding in it, and the list. What outlives a read of the status
// (the open documents, the drawer) lives above, in the workspace's library; a view used alone brings
// its own.
export function KnowledgeCard({ transport }: { transport: Transport }) {
  const outer = useContext(LibraryContext);
  return outer ? <KnowledgeView /> : (
    <StandaloneLibrary transport={transport}>
      <KnowledgeView />
    </StandaloneLibrary>
  );
}

function KnowledgeView() {
  const t = useTranslate();
  const library = useLibrary();

  // The panel's refresh reads the counts quietly: what the owner has open stays where it is.
  return (
    <CardFrame
      title={t("knowledge_title")}
      quietTitle
      fix={<OpenSystem />}
      state={library.status}
      onReload={library.reload}
      onRefresh={library.poll}
      refreshing={library.refreshing}
    >
      {(status) => (status.off ? <><OffNotice reason={status.reason} /><OpenSystem /></> : <KnowledgeBody status={status} />)}
    </CardFrame>
  );
}

function KnowledgeBody({ status }: { status: KnowledgeStatus }) {
  const { transport, open, changed, revision } = useLibrary();
  const search = useKnowledgeSearch(transport);
  // An empty Library has nothing to search and nothing to count: the list says so alone. That is
  // known once the list has answered, so a Library that may be empty waits for it.
  const [total, setTotal] = useState<number>();
  const bare = status.documents === 0 && (total === undefined || total === 0);

  return (
    <>
      {!bare && (
        <div className="library-tools">
          <SearchForm search={search} />
        </div>
      )}
      {!bare && <SearchOutcome search={search} onOpen={(hit) => open({ id: hit.sourceId, title: hit.title }, undefined, false)} />}
      <KnowledgeDocumentsPanel transport={transport} revision={revision} onChanged={changed} onOpen={open} status={status} onTotal={setTotal} />
    </>
  );
}

// The search lives in the body, not in its form: the field sits in the toolbar beside Add, and what it
// finds is drawn below the toolbar, so nothing the search returns can push Add out of place.
function useKnowledgeSearch(transport: Transport) {
  const [query, setQuery] = useState("");
  const run = useCallback((asked: string) => searchKnowledge(transport, asked), [transport]);
  const { state, start } = useAction(run);
  return { query, setQuery, state, start };
}
type KnowledgeSearchState = ReturnType<typeof useKnowledgeSearch>;

function SearchForm({ search }: { search: KnowledgeSearchState }) {
  const t = useTranslate();
  const inputId = useId();
  const { query, setQuery, state, start } = search;
  const searching = state.phase === "loading";
  const asked = query.trim();

  return (
    <form
      className="search"
      onSubmit={(event) => {
        event.preventDefault();
        if (asked !== "" && !searching) start(asked);
      }}
    >
      <label htmlFor={inputId} className="sr-only">{t("knowledge_ask")}</label>
      <input
        id={inputId}
        type="search"
        data-library-search=""
        placeholder={t("knowledge_ask")}
        value={query}
        maxLength={MAX_QUERY_CHARS}
        autoComplete="off"
        onChange={(event) => setQuery(event.target.value)}
      />
      <button type="submit" disabled={searching || asked === ""}>
        {t("knowledge_search")}
      </button>
    </form>
  );
}

function SearchOutcome({ search, onOpen }: { search: KnowledgeSearchState; onOpen: (hit: KnowledgeHit) => void }) {
  const t = useTranslate();
  const { state } = search;
  return (
    <div aria-live="polite">
      {state.phase === "loading" && <p role="status">{t("knowledge_searching")}</p>}
      {state.phase === "error" && <Problem error={state.error} />}
      {state.phase === "ready" && <SearchResults search={state.data} onOpen={onOpen} />}
    </div>
  );
}

function SearchResults({ search, onOpen }: { search: KnowledgeSearch | Off; onOpen: (hit: KnowledgeHit) => void }) {
  const t = useTranslate();
  const { fixed } = useNumbers();
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
              {hit.sourceId && <>{" "}<button type="button" aria-label={t("documents_open_named", { title: hit.title || hit.sourceId })}
                onClick={() => onOpen(hit)}>{t("documents_open")}</button></>}
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
