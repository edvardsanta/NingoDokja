import { useId, useRef, useState, type KeyboardEvent } from "react";

import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { DigestCard } from "./digest.js";
import { KnowledgeCard } from "./knowledge.js";
import { WorkspacePanel } from "./workspace_panel.js";

// What Ningo knows: the documents it keeps, and the sources it reads to learn more. Reading is how
// knowledge comes in, not a place of its own, so both live in one workspace. A view asks its service
// the first time it opens and is then kept, hidden while the other is shown, so a search or an open
// document survives a look at the sources.
const VIEWS: readonly { id: "documents" | "sources"; labelId: MessageId }[] = [
  { id: "documents", labelId: "knowledge_view_documents" },
  { id: "sources", labelId: "knowledge_view_sources" },
];

export function KnowledgeWorkspace({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const [view, setView] = useState<(typeof VIEWS)[number]["id"]>("documents");
  const [opened, setOpened] = useState(() => new Set([view]));
  const baseId = useId();
  const tabs = useRef(new Map<string, HTMLButtonElement>());
  const show = (id: (typeof VIEWS)[number]["id"]) => {
    setView(id);
    setOpened((current) => (current.has(id) ? current : new Set([...current, id])));
  };
  const currentLabel = VIEWS.find((each) => each.id === view)?.labelId ?? "knowledge_view_documents";
  // The views are places, so they are tabs: the arrows move between them, as in the sidebar.
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return;
    event.preventDefault();
    const index = VIEWS.findIndex((each) => each.id === view) + (event.key === "ArrowRight" ? 1 : -1);
    const next = VIEWS[(index + VIEWS.length) % VIEWS.length];
    if (next) {
      show(next.id);
      tabs.current.get(next.id)?.focus();
    }
  };
  const views = (
    <div className="vtabs" role="tablist" aria-label={t("nav_library")} onKeyDown={onKeyDown}>
      {VIEWS.map((each) => (
        <button
          key={each.id}
          ref={(node) => {
            if (node) tabs.current.set(each.id, node);
            else tabs.current.delete(each.id);
          }}
          type="button"
          role="tab"
          className="vtab"
          id={`${baseId}-tab-${each.id}`}
          aria-selected={view === each.id}
          aria-controls={`${baseId}-view-${each.id}`}
          tabIndex={view === each.id ? 0 : -1}
          onClick={() => show(each.id)}
        >
          {t(each.labelId)}
        </button>
      ))}
    </div>
  );
  return (
    <WorkspacePanel trail={[t("nav_library"), t(currentLabel)]} views={views}>
      {VIEWS.map((each) => (
        <div key={each.id} role="tabpanel" id={`${baseId}-view-${each.id}`} aria-labelledby={`${baseId}-tab-${each.id}`} hidden={view !== each.id}>
          {opened.has(each.id) && (each.id === "documents" ? <KnowledgeCard transport={transport} /> : <DigestCard transport={transport} />)}
        </div>
      ))}
    </WorkspacePanel>
  );
}
