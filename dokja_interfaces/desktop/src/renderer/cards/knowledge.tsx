import { useCallback, useEffect, useId, useState, type DragEvent } from "react";

import type { KnowledgeDocument, KnowledgeHit, KnowledgeSearch, KnowledgeStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice, Problem } from "./card_frame.js";
import { DocumentView } from "./document_view.js";
import { Drawer } from "./drawer.js";
import { IndexPending, KnowledgeDocumentsPanel } from "./knowledge_documents.js";
import { AddToBase, type Dropped } from "./knowledge_add.js";
import type { AddMode } from "./knowledge_add_model.js";
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
  const { state, reload, poll, refreshing } = useCard(load);

  // The panel's refresh reads the counts quietly: what the owner has open stays where it is.
  return (
    <CardFrame title={t("knowledge_title")} state={state} onReload={reload} onRefresh={poll} refreshing={refreshing}>
      {(status) =>
        status.off ? (
          <OffNotice reason={status.reason} />
        ) : (
          <KnowledgeBody transport={transport} initial={status} />
        )
      }
    </CardFrame>
  );
}

// The base's own view, always the first tab.
const BASE = "";
const MAX_OPEN_DOCUMENTS = 8;
type OpenDocument = { id: string; title: string; at?: number; external: boolean; classify: boolean };

// Lights the view while files are dragged over it and hands over the first file dropped. Entering and
// leaving fire for every child crossed, so they are counted rather than trusted one by one. Only a
// drag that carries files counts: dragged text is left to the fields.
function useFileDrop(onFile: (file: File) => void) {
  const [depth, setDepth] = useState(0);
  const carriesFiles = (event: DragEvent) => Array.from(event.dataTransfer.types).includes("Files");
  const handlers = {
    onDragEnter: (event: DragEvent) => {
      if (carriesFiles(event)) setDepth((value) => value + 1);
    },
    onDragLeave: (event: DragEvent) => {
      if (carriesFiles(event)) setDepth((value) => Math.max(0, value - 1));
    },
    onDragOver: (event: DragEvent) => {
      if (carriesFiles(event)) event.preventDefault();
    },
    onDrop: (event: DragEvent) => {
      setDepth(0);
      // the form took it already
      if (event.defaultPrevented) return;
      const file = event.dataTransfer.files[0];
      if (!file) return;
      event.preventDefault();
      onFile(file);
    },
  };
  return { dragging: depth > 0, handlers };
}

// The counts are read when the card opens and with the panel's refresh; adding a document reads them
// again, quietly, so they do not stay behind what the search now finds.
function KnowledgeBody({ transport, initial }: { transport: Transport; initial: KnowledgeStatus }) {
  const t = useTranslate();
  const [status, setStatus] = useState(initial);
  // a quiet refresh hands over new counts while this body stays mounted
  useEffect(() => setStatus(initial), [initial]);
  const search = useKnowledgeSearch(transport);
  const [revision, setRevision] = useState(0);
  const refresh = useCallback(() => {
    setRevision((value) => value + 1);
    void loadKnowledge(transport).then((result) => {
      if (result.ok && !result.data.off) setStatus(result.data);
    });
  }, [transport]);

  // Documents opened from the list, each in a tab after the base's own. Opening one already open goes
  // back to its tab; the oldest goes first past the limit.
  const [documents, setDocuments] = useState<OpenDocument[]>([]);
  const [current, setCurrent] = useState(BASE);
  // Classify… opens the document with its classification shown; asking again for an open document
  // shows that too. A search hit has no place in the list, so it opens without one.
  const open = useCallback((document: Pick<KnowledgeDocument, "id" | "title">, at: number | undefined, external: boolean, classify = false) => {
    setDocuments((previous) =>
      previous.some((each) => each.id === document.id)
        ? previous.map((each) => each.id === document.id && classify ? { ...each, classify } : each)
        : [...previous, { id: document.id, title: document.title || document.id, at, external, classify }].slice(-MAX_OPEN_DOCUMENTS),
    );
    setCurrent(document.id);
  }, []);
  const close = (id: string) => {
    setDocuments((previous) => previous.filter((each) => each.id !== id));
    setCurrent((shown) => (shown === id ? BASE : shown));
  };

  // Adding is one action of the view, not the view itself: the form opens on demand. A file dropped
  // anywhere on the view opens it and goes to it, as if it had been dropped on the form; the
  // Library's tab comes to the front so the form can be seen.
  const [adding, setAdding] = useState(false);
  // The drawer is made again each time it opens; it opens on the way of adding last used. A file
  // dropped before is forgotten when it closes, or it would take that way back on every opening.
  const [addMode, setAddMode] = useState<AddMode>("note");
  const [dropped, setDropped] = useState<Dropped>();
  const drop = useFileDrop((file) => {
    setCurrent(BASE);
    setAdding(true);
    setDropped((previous) => ({ file, count: (previous?.count ?? 0) + 1 }));
  });

  return (
    <div className="drop-zone" data-dragging={drop.dragging ? "" : undefined} {...drop.handlers}>
      {drop.dragging && (
        <p className="drop-hint" aria-hidden="true">
          {t("add_drop_hint")}
        </p>
      )}
      {documents.length > 0 && (
        <div className="view-tabs" role="group" aria-label={t("documents_tabs_label")}>
          <button type="button" className="view-tab" aria-current={current === BASE ? "true" : undefined} onClick={() => setCurrent(BASE)}>
            {t("view_base")}
          </button>
          {documents.map((document) => (
            <span key={document.id} className="view-tab-pair">
              <button
                type="button"
                className="view-tab"
                aria-current={current === document.id ? "true" : undefined}
                onClick={() => setCurrent(document.id)}
              >
                {document.title}
              </button>
              <button
                type="button"
                className="view-tab-close"
                aria-label={t("view_close_named", { title: document.title })}
                onClick={() => close(document.id)}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      <div hidden={current !== BASE}>
        <div className="library-tools">
          <SearchForm search={search} />
          <button type="button" className="library-add primary" aria-haspopup="dialog" aria-expanded={adding} onClick={() => setAdding(true)}>
            {t("add_title")}
          </button>
        </div>
        <SearchOutcome search={search} onOpen={(hit) => open({ id: hit.sourceId, title: hit.title }, undefined, false)} />
        {adding && <Drawer title={t("add_title")} onClose={() => { setAdding(false); setDropped(undefined); }}>
          <AddToBase transport={transport} onAdded={refresh} dropped={dropped} titled={false} startMode={addMode} onMode={setAddMode} />
        </Drawer>}
        <KnowledgeStatusLines status={status} />
        <IndexPending transport={transport} pending={status.pendingEmbeddings} onChanged={refresh} />
        <KnowledgeDocumentsPanel transport={transport} revision={revision} onChanged={refresh} onOpen={open} />
      </div>
      {documents.map((document) => (
        <div key={document.id} hidden={current !== document.id}>
          <DocumentView transport={transport} id={document.id} at={document.at} external={document.external} classify={document.classify} />
        </div>
      ))}
    </div>
  );
}

function KnowledgeStatusLines({ status }: { status: KnowledgeStatus }) {
  const t = useTranslate();
  const similarity = similarityOf(status);
  return (
    <>
      {status.documents === 0 ? (
        <p className="note" data-tone="off">
          {t("knowledge_empty")}
        </p>
      ) : (
        <p>{t("knowledge_counts", { documents: status.documents, chunks: status.chunks })}</p>
      )}
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
