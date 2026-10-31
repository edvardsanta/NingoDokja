import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

import { isRecord, text } from "../../shared/records.js";
import type { KnowledgeDocument, KnowledgeDocuments, KnowledgeStatus, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import { OffNotice, OpenSystem, Problem } from "./card_frame.js";
import { MenuRow, useRowMenu, type MenuItem } from "./context_menu.js";
import { KnowledgeLine } from "./knowledge_line.js";
import { similarityOf } from "./knowledge_model.js";
import { Button, VoiceBlock } from "./ui.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";
import { useWorkspaceRefresh } from "./workspace_panel.js";

const PAGE_SIZE = 20;
const count = (value: unknown): number =>
  typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : 0;
const unexpected = (): LoadResult<never> => ({ ok: false, error: { code: "unexpected", message: "unexpected knowledge reply" } });

export { IndexPending } from "./knowledge_line.js";

export function parseDocuments(value: unknown): KnowledgeDocuments | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (!Array.isArray(value.documents) || typeof value.total !== "number") return undefined;
  const documents: KnowledgeDocument[] = value.documents.filter(isRecord).map((item) => ({
    id: text(item.id), title: text(item.title), kind: text(item.kind), reference: text(item.reference),
    tags: Array.isArray(item.tags) ? item.tags.map(text) : [], chunks: count(item.chunks),
    embedded: count(item.embedded), updatedAt: text(item.updatedAt),
    ...(typeof item.text === "string" ? { text: item.text, truncated: item.truncated === true } : {}),
  })).filter((item) => item.id !== "");
  return { documents, total: count(value.total), offset: count(value.offset) };
}

// The list of what the Library holds, with the line above it: how much, how well it is indexed and the
// filter. `onOpen` opens a document in a tab of its own, given where it sits in the list: its text is
// asked for there, one document at a time. `status` is what the service reports; with it the panel can
// tell an empty Library from a list that leaves out what Ningo keeps for himself. `onTotal` hands the
// list's total up as soon as it is known. `results` takes the list's place while a search is shown
// (under the same line, which says how well search works): the list, its filter and its pager wait
// where they were.
export function KnowledgeDocumentsPanel({ transport, revision, onChanged, onOpen, status, onTotal, results }: {
  transport: Transport; revision: number; onChanged: () => void;
  onOpen?: (document: KnowledgeDocument, at: number, external: boolean, classify: boolean) => void;
  status?: KnowledgeStatus;
  onTotal?: (total: number | undefined) => void;
  results?: ReactNode;
}) {
  const t = useTranslate();
  const [offset, setOffset] = useState(0);
  // What Ningo keeps for himself (a meme's hashtag) is left out unless the owner asks to see it.
  const [internal, setInternal] = useState(false);
  const busy = useRef(false);
  const load = useCallback(async (at: number): Promise<LoadResult<KnowledgeDocuments | Off>> => {
    const answer = await transport.request("knowledge.list", { limit: PAGE_SIZE, offset: at, ...(internal ? {} : { external: true }) }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    const page = parseDocuments(answer.result);
    return page ? { ok: true, data: page } : unexpected();
  }, [transport, internal]);
  const listing = useAction(load);
  const remove = useCallback(async (id: string): Promise<LoadResult<{ deleted: boolean } | Off>> => {
    if (busy.current) return unexpected();
    busy.current = true;
    try {
      const answer = await transport.request("knowledge.delete", { id }, { timeoutMs: 30_000 });
      if (!answer.ok) return answer;
      if (!isRecord(answer.result)) return unexpected();
      if (answer.result.off === true) return { ok: true, data: { off: true, reason: text(answer.result.reason) } };
      if (typeof answer.result.deleted !== "boolean") return unexpected();
      // Return to the first page: deleting the last item must not strand an empty final page.
      setOffset(0);
      onChanged();
      return { ok: true, data: { deleted: answer.result.deleted } };
    } finally {
      busy.current = false;
    }
  }, [transport, onChanged]);
  const deletion = useAction(remove);
  const removing = deletion.state.phase === "loading";
  // The list is what the Library is for, so it is read when the view opens, and again with the
  // panel's refresh.
  useEffect(() => {
    listing.start(offset);
  }, [offset, revision, internal, listing.start]);
  const reread = useCallback(() => listing.start(offset), [listing.start, offset]);
  useWorkspaceRefresh(reread, listing.state.phase === "loading");

  // The page stays on screen while the next one is read: a refresh, an added document or a change of
  // page does not blank the list, and the row that has the focus keeps it.
  const [kept, setKept] = useState<KnowledgeDocuments | Off>();
  useEffect(() => {
    if (listing.state.phase === "ready") setKept(listing.state.data);
    else if (listing.state.phase === "error") setKept(undefined);
  }, [listing.state]);
  const shown = listing.state.phase === "ready" ? listing.state.data : listing.state.phase === "loading" ? kept : undefined;
  const page = shown && !shown.off ? shown : undefined;
  const firstRead = listing.state.phase === "loading" && kept === undefined;
  useEffect(() => { onTotal?.(page?.total); }, [page?.total]);

  // One stop of the tab key for the whole list: the row that last had the focus. The arrows move it.
  const [active, setActive] = useState(0);
  useEffect(() => { setActive(0); }, [offset, internal]);
  const list = useRef<HTMLUListElement>(null);
  const moveRows = (event: KeyboardEvent<HTMLUListElement>) => {
    const target = { ArrowDown: 1, ArrowUp: -1, Home: -Infinity, End: Infinity }[event.key];
    if (target === undefined || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    const rows = [...(list.current?.children ?? [])] as HTMLElement[];
    const from = rows.findIndex((row) => row.contains(event.target as Node));
    if (from < 0) return;
    event.preventDefault();
    const to = Math.max(0, Math.min(rows.length - 1, Number.isFinite(target) ? from + target : target < 0 ? 0 : rows.length - 1));
    rows[to]?.focus();
  };

  // The Library is empty when it has nothing at all; when only what Ningo keeps for himself is there,
  // it says nothing was brought in but still lets the owner see what is kept.
  const total = page?.total;
  const empty = total === 0 && (status === undefined || status.documents === 0);
  const nothingBrought = total === 0 && !empty && !internal;
  const hidden = page && status && !internal ? status.documents - page.total : 0;
  const similar = status !== undefined && similarityOf(status) === "on";
  const searching = results !== undefined;
  const row = (document: KnowledgeDocument, index: number) => (
    <DocumentRow key={document.id} document={document} removing={removing} similar={similar}
      tabIndex={Math.min(active, (page?.documents.length ?? 1) - 1) === index ? 0 : -1} onFocus={() => setActive(index)}
      onOpen={onOpen && page ? (classify) => onOpen(document, page.offset + index, !internal, classify) : undefined}
      onDelete={() => { if (!removing) deletion.start(document.id); }} />
  );

  return (
    <section className="library-documents" aria-label={t("documents_title")} aria-busy={listing.state.phase === "loading"}>
      <h3 className="sr-only">{t("documents_title")}</h3>
      {firstRead && <p role="status">{t("loading")}</p>}
      {listing.state.phase === "error" && <>
        <Problem error={listing.state.error} />
        <div className="problem-actions">
          <Button extra="retry" onClick={reread}>{t("retry")}</Button>
          <OpenSystem />
        </div>
      </>}
      {shown?.off && <><OffNotice reason={shown.reason} /><OpenSystem /></>}
      {page && empty && <VoiceBlock voice={t("library_voice_empty")} next={t("library_empty_next")} />}
      {page && !empty && <>
        {nothingBrought && !searching && <VoiceBlock voice={t("library_voice_empty")} next={t("library_empty_next")} />}
        <KnowledgeLine transport={transport} status={status} facts={!nothingBrought} internal={internal}
          onInternal={searching ? undefined : (next) => { setInternal(next); setOffset(0); }} hidden={searching ? 0 : hidden} onChanged={onChanged} />
        {searching ? results : <>
        <div className="line-feedback" aria-live="polite">
          {removing && <p role="status">{t("documents_removing")}</p>}
          {deletion.state.phase === "error" && (deletion.state.error.code === "timeout"
            ? <p role="alert">{t("documents_delete_timeout")}</p> : <Problem error={deletion.state.error} />)}
          {deletion.state.phase === "ready" && ("off" in deletion.state.data
            ? <OffNotice reason={deletion.state.data.reason} />
            : <p>{deletion.state.data.deleted ? t("documents_deleted") : t("documents_absent")}</p>)}
        </div>
        {page.total > 0 && page.documents.length === 0 && <p>{t("documents_empty")}</p>}
        {page.documents.length > 0 && <ul ref={list} className="hits documents" onKeyDown={moveRows}>
          {page.documents.map(row)}
        </ul>}
        {page.total > PAGE_SIZE && <div className="pager">
          <Button variant="quiet" size="compact" disabled={removing || page.offset === 0}
            onClick={() => setOffset(Math.max(0, page.offset - PAGE_SIZE))}><span aria-hidden="true">‹ </span>{t("documents_previous")}</Button>
          <span>{t("documents_range", { from: page.offset + 1, to: page.offset + page.documents.length, total: page.total })}</span>
          <Button variant="quiet" size="compact" disabled={removing || page.offset + page.documents.length >= page.total || page.documents.length === 0}
            onClick={() => setOffset(page.offset + PAGE_SIZE)}>{t("documents_next")}<span aria-hidden="true"> ›</span></Button>
        </div>}
        </>}
      </>}
      {searching && !page && !empty && results}
    </section>
  );
}

// One document of the list, in two lines: its title, which opens it, and what it is, where it came
// from and when it changed. Open, Classify… and Delete… are in its menu (right click, the Menu key or
// ⋯). Delete is still asked in the shell's own window. How well it is indexed is a state, so it shows
// only when it is not complete, and only where similarity works to complete it.
function DocumentRow({ document, removing, similar, tabIndex, onFocus, onOpen, onDelete }: {
  document: KnowledgeDocument; removing: boolean; similar: boolean; tabIndex: number; onFocus: () => void;
  onOpen?: (classify: boolean) => void; onDelete: () => void;
}) {
  const t = useTranslate();
  const { ago } = useTimes();
  const title = document.title || document.id;
  const items: MenuItem[] = [
    ...(onOpen ? [{ label: t("documents_open"), run: () => onOpen(false) }, { label: t("documents_classify"), run: () => onOpen(true) }] : []),
    { label: t("documents_delete_ask"), run: onDelete, disabled: removing },
  ];
  const menu = useRowMenu(items, title);
  const rest = [document.reference, document.tags.join(", ")].filter(Boolean);
  const when = document.updatedAt === "" ? "" : ago(document.updatedAt);
  return <MenuRow menu={menu} tabIndex={tabIndex} onFocus={onFocus} onActivate={onOpen ? () => onOpen(false) : undefined}>
    <p className="document-title">{onOpen
      // the row is the stop of the tab key; the title is still a button to click
      ? <button type="button" className="row-title" tabIndex={-1} aria-label={t("documents_open_named", { title })} onClick={() => onOpen(false)}><strong>{title}</strong></button>
      : <strong>{title}</strong>}</p>
    <p className="detail document-meta">
      <span>
        {/* the id is the operator's: in the tooltip of the kind, not in the row */}
        {document.kind && <span title={document.id}>{document.kind}</span>}
        {rest.map((part, index) => <span key={index}>{document.kind || index > 0 ? " · " : ""}{part}</span>)}
        {similar && document.embedded < document.chunks && (
          <span className="note" data-tone="unchecked">{t("documents_passages", { indexed: document.embedded, count: document.chunks })}</span>
        )}
      </span>
      {when && <span className="document-time">{t("documents_updated", { ago: when })}</span>}
    </p>
  </MenuRow>;
}
