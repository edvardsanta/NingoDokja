import { useCallback, useEffect, useState } from "react";

import type { KnowledgeDocument, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTranslate } from "../i18n/context.js";
import { OffNotice, Pending, Problem } from "./card_frame.js";
import { parseDocuments } from "./knowledge_documents.js";
import { KnowledgeClassificationPanel } from "./knowledge_classification.js";
import { useCard, type LoadResult } from "./use_card.js";

// The document that sits at `at` in the list, with its text, or word that another one sits there now
// (the list changed since it was shown).
type Opened = { document: KnowledgeDocument } | { moved: true } | Off;

// `external` counts the position among what came from outside only, as the list it came from did.
export async function loadDocument(transport: Transport, id: string, at: number, external = false): Promise<LoadResult<Opened>> {
  const answer = await transport.request("knowledge.list", { limit: 1, offset: at, include_text: true, ...(external ? { external: true } : {}) }, { timeoutMs: 15_000 });
  if (!answer.ok) return answer;
  const page = parseDocuments(answer.result);
  if (!page) return { ok: false, error: { code: "unexpected", message: "unexpected knowledge reply" } };
  if (page.off) return { ok: true, data: page };
  const document = page.documents[0];
  return { ok: true, data: document && document.id === id ? { document } : { moved: true } };
}

// One document of the research base, in a tab of its own: what the list says of it, and its text.
// The text is what the service rebuilds from the passages it stored, so it says so: close to the
// original, not a faithful copy of it. It is shown as text, never as markup.
export function DocumentView({ transport, id, at, external = false, classify = false }: { transport: Transport; id: string; at: number; external?: boolean; classify?: boolean }) {
  const t = useTranslate();
  const [classificationOpen, setClassificationOpen] = useState(classify);
  // Classify… on a document already open shows its classification.
  useEffect(() => { if (classify) setClassificationOpen(true); }, [classify]);
  const { fixed } = useNumbers();
  const load = useCallback(() => loadDocument(transport, id, at, external), [transport, id, at, external]);
  const { state, reload } = useCard(load);

  return (
    <section className="document-view" aria-busy={state.phase === "loading"}>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && <Problem error={state.error} />}
      {state.phase === "ready" && "off" in state.data && state.data.off && <OffNotice reason={state.data.reason} />}
      {state.phase === "ready" && "moved" in state.data && (
        <p className="note" data-tone="unchecked">
          {t("document_moved")}
        </p>
      )}
      {state.phase === "ready" && "document" in state.data && (() => {
        const document = state.data.document;
        return (
          <>
            <h3>{document.title || document.id}</h3>
            <p className="detail">
              {[document.id, document.kind, document.reference, document.tags.join(", ")].filter(Boolean).join(" · ")}
            </p>
            <p className="detail">{t("documents_passages", { indexed: document.embedded, count: document.chunks })}</p>
            <button type="button" aria-expanded={classificationOpen} onClick={() => setClassificationOpen((open) => !open)}>{t("classification_title")}</button>
            {classificationOpen && <KnowledgeClassificationPanel key={document.id} transport={transport} id={document.id} />}
            <p className="note" data-tone="unchecked">
              {t("document_rebuilt")}
            </p>
            {document.text ? (
              <pre className="document-text">{document.text}</pre>
            ) : (
              <p>{t("document_no_text")}</p>
            )}
            {document.truncated && (
              <p className="note" data-tone="off">
                {t("document_truncated", { count: fixed(document.text?.length ?? 0, 0) })}
              </p>
            )}
          </>
        );
      })()}
      {/* the card's own button refreshes the base; this one only asks again for a text that failed */}
      {state.phase === "error" && (
        <button type="button" onClick={reload}>
          {t("retry")}
        </button>
      )}
    </section>
  );
}
