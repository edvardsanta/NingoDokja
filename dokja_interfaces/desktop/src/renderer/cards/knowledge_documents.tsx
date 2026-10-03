import { useCallback, useEffect, useRef, useState } from "react";

import { isRecord, text } from "../../shared/records.js";
import type { KnowledgeDocument, KnowledgeDocuments, Off, ReindexResult } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { OffNotice, Problem } from "./card_frame.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

const PAGE_SIZE = 20;
const count = (value: unknown): number =>
  typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : 0;
const unexpected = (): LoadResult<never> => ({ ok: false, error: { code: "unexpected", message: "unexpected knowledge reply" } });

export function parseDocuments(value: unknown): KnowledgeDocuments | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: text(value.reason) };
  if (!Array.isArray(value.documents) || typeof value.total !== "number") return undefined;
  const documents: KnowledgeDocument[] = value.documents.filter(isRecord).map((item) => ({
    id: text(item.id), title: text(item.title), kind: text(item.kind), reference: text(item.reference),
    tags: Array.isArray(item.tags) ? item.tags.map(text) : [], chunks: count(item.chunks),
    embedded: count(item.embedded), updatedAt: text(item.updatedAt),
  })).filter((item) => item.id !== "");
  return { documents, total: count(value.total), offset: count(value.offset) };
}

export function KnowledgeDocumentsPanel({ transport, revision, onChanged }: {
  transport: Transport; revision: number; onChanged: () => void;
}) {
  const t = useTranslate();
  const [opened, setOpened] = useState(false);
  const [offset, setOffset] = useState(0);
  const busy = useRef(false);
  const load = useCallback(async (at: number): Promise<LoadResult<KnowledgeDocuments | Off>> => {
    const answer = await transport.request("knowledge.list", { limit: PAGE_SIZE, offset: at }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    const page = parseDocuments(answer.result);
    return page ? { ok: true, data: page } : unexpected();
  }, [transport]);
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
  useEffect(() => {
    if (opened) listing.start(offset);
  }, [opened, offset, revision, listing.start]);
  const page = listing.state.phase === "ready" ? listing.state.data : undefined;

  return (
    <section aria-label={t("documents_title")}>
      <h3>{t("documents_title")}</h3>
      <button type="button" disabled={removing || listing.state.phase === "loading"} onClick={() => {
        if (!opened) setOpened(true);
        else listing.start(offset);
      }}>{t("documents_load")}</button>
      {listing.state.phase === "loading" && <p role="status">{t("loading")}</p>}
      {listing.state.phase === "error" && <Problem error={listing.state.error} />}
      {page?.off && <OffNotice reason={page.reason} />}
      {page && !page.off && <>
        <p>{t("documents_total", { count: page.total })}</p>
        {page.documents.length === 0 && <p>{t("documents_empty")}</p>}
        <ul className="hits">
          {page.documents.map((document) => <li key={document.id}>
            <p><strong>{document.title || document.id}</strong></p>
            <p className="detail">{[document.id, document.kind, document.reference, document.tags.join(", ")].filter(Boolean).join(" · ")}</p>
            <p className="detail">{t("documents_passages", { indexed: document.embedded, count: document.chunks })}</p>
            <button type="button" disabled={removing} aria-label={t("documents_delete_named", { title: document.title || document.id })}
              onClick={() => { if (!removing) deletion.start(document.id); }}>{t("documents_delete")}</button>
          </li>)}
        </ul>
        <div className="pager">
          <button type="button" disabled={removing || page.offset === 0} onClick={() => setOffset(Math.max(0, page.offset - PAGE_SIZE))}>{t("documents_previous")}</button>
          <button type="button" disabled={removing || page.offset + page.documents.length >= page.total || page.documents.length === 0}
            onClick={() => setOffset(page.offset + PAGE_SIZE)}>{t("documents_next")}</button>
        </div>
      </>}
      <div aria-live="polite">
        {removing && <p role="status">{t("documents_removing")}</p>}
        {deletion.state.phase === "error" && (deletion.state.error.code === "timeout"
          ? <p role="alert">{t("documents_delete_timeout")}</p> : <Problem error={deletion.state.error} />)}
        {deletion.state.phase === "ready" && ("off" in deletion.state.data
          ? <OffNotice reason={deletion.state.data.reason} />
          : <p>{deletion.state.data.deleted ? t("documents_deleted") : t("documents_absent")}</p>)}
      </div>
    </section>
  );
}

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
  return <section aria-label={t("index_title")}>
    {pending > 0 && <button type="button" disabled={state.phase === "loading"} onClick={() => start(undefined)}>{t("index_title")}</button>}
    <div aria-live="polite">
      {state.phase === "loading" && <p role="status">{t("index_running")}</p>}
      {state.phase === "error" && (state.error.code === "timeout"
        ? <p role="alert">{t("index_timeout")}</p> : <Problem error={state.error} />)}
      {state.phase === "ready" && (state.data.off ? <OffNotice reason={state.data.reason} /> : <>
        <p>{t("index_result", { count: state.data.embedded, remaining: state.data.remaining })}</p>
        {state.data.degraded && <p className="note" data-tone="unchecked">{t("index_degraded")} {state.data.reason}</p>}
      </>)}
    </div>
  </section>;
}
