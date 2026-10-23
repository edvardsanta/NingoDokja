import { useState } from "react";

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
  const show = (id: (typeof VIEWS)[number]["id"]) => {
    setView(id);
    setOpened((current) => (current.has(id) ? current : new Set([...current, id])));
  };
  const currentLabel = VIEWS.find((each) => each.id === view)?.labelId ?? "knowledge_view_documents";
  const views = (
    <div className="scope knowledge-views" role="group" aria-label={t("nav_library")}>
      {VIEWS.map((each) => (
        <button key={each.id} type="button" aria-pressed={view === each.id} onClick={() => show(each.id)}>
          {t(each.labelId)}
        </button>
      ))}
    </div>
  );
  return (
    <WorkspacePanel trail={[t("nav_library"), t(currentLabel)]} views={views}>
      {opened.has("documents") && <div hidden={view !== "documents"}><KnowledgeCard transport={transport} /></div>}
      {opened.has("sources") && <div hidden={view !== "sources"}><DigestCard transport={transport} /></div>}
    </WorkspacePanel>
  );
}
