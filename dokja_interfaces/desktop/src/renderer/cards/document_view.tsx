import { useCallback, useEffect, useState } from "react";

import type { KnowledgeDocument, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTimes, useTranslate } from "../i18n/context.js";
import { OffNotice, Pending, Problem } from "./card_frame.js";
import { parseDocuments } from "./knowledge_documents.js";
import { KnowledgeClassificationPanel } from "./knowledge_classification.js";
import { Button } from "./ui.js";
import { useCard, type LoadResult } from "./use_card.js";
import { useWorkspaceRefresh } from "./workspace_panel.js";

// The document, with its text, or word that it is no longer there (it was deleted since its tab opened).
type Opened = { document: KnowledgeDocument } | { gone: true } | Off;

// A document is read by its id, whichever way it was opened (a row of the list, a hit, the menu's
// Classify…): where it sat in a list says nothing once the list changes.
export async function loadDocument(transport: Transport, id: string): Promise<LoadResult<Opened>> {
  const answer = await transport.request("knowledge.list", { source_id: id, include_text: true }, { timeoutMs: 15_000 });
  if (!answer.ok) return answer;
  const page = parseDocuments(answer.result);
  if (!page) return { ok: false, error: { code: "unexpected", message: "unexpected knowledge reply" } };
  if (page.off) return { ok: true, data: page };
  const document = page.documents.find((each) => each.id === id);
  return { ok: true, data: document ? { document } : { gone: true } };
}

// One document of the Library, in a tab of its own: its title, what the list says of it in one line,
// and its text. The text is what the service rebuilds from the passages it stored, so it says so,
// quietly and under the text: close to the original, not a faithful copy of it. It is shown as
// text, never as markup, in a column a text is comfortable to read in.
export function DocumentView({ transport, id, classify = false }: { transport: Transport; id: string; classify?: boolean }) {
  const t = useTranslate();
  const { ago } = useTimes();
  const { fixed } = useNumbers();
  const [classificationOpen, setClassificationOpen] = useState(classify);
  // Classify… on a document already open shows its classification.
  useEffect(() => { if (classify) setClassificationOpen(true); }, [classify]);
  const load = useCallback(() => loadDocument(transport, id), [transport, id]);
  const { state, reload, poll, refreshing } = useCard(load);
  // The panel's refresh reads the text again where it stands.
  useWorkspaceRefresh(poll, refreshing || state.phase === "loading");

  return (
    <section className="document-view" aria-busy={state.phase === "loading"}>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && (
        <>
          <Problem error={state.error} />
          {/* the panel's own refresh reads everything; this one only asks again for a text that failed */}
          <div className="problem-actions">
            <Button extra="retry" onClick={reload}>{t("retry")}</Button>
          </div>
        </>
      )}
      {state.phase === "ready" && "off" in state.data && state.data.off && <OffNotice reason={state.data.reason} />}
      {state.phase === "ready" && "gone" in state.data && (
        <p className="note" data-tone="off">
          {t("document_gone")}
        </p>
      )}
      {state.phase === "ready" && "document" in state.data && (() => {
        const document = state.data.document;
        const when = document.updatedAt === "" ? "" : ago(document.updatedAt);
        // what it is, where it came from and when it changed; the id is the operator's, in the tooltip
        const meta = [
          document.kind,
          document.reference,
          document.tags.join(", "),
          when === "" ? "" : t("documents_updated", { ago: when }),
          t("documents_passages", { indexed: document.embedded, count: document.chunks }),
        ].filter(Boolean);
        return (
          <>
            <header className="document-head">
              <h3 title={document.id}>{document.title || document.id}</h3>
              <button
                type="button"
                className="disclosure"
                aria-expanded={classificationOpen}
                onClick={() => setClassificationOpen((open) => !open)}
              >
                {t("classification_title")}
                <span aria-hidden="true"> {classificationOpen ? "▾" : "▸"}</span>
              </button>
            </header>
            <p className="detail document-line">{meta.join(" · ")}</p>
            {classificationOpen && <KnowledgeClassificationPanel key={document.id} transport={transport} id={document.id} />}
            {document.text ? (
              <>
                <pre className="document-text">{document.text}</pre>
                {document.truncated && (
                  <p className="note" data-tone="off">
                    {t("document_truncated", { count: fixed(document.text.length, 0) })}
                  </p>
                )}
                <p className="note" data-tone="off">
                  {t("document_rebuilt")}
                </p>
              </>
            ) : (
              <p>{t("document_no_text")}</p>
            )}
          </>
        );
      })()}
    </section>
  );
}
