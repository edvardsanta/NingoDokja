import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { DigestCard } from "./digest.js";
import { KnowledgeCard } from "./knowledge.js";
import { useViewTabs } from "./view_tabs.js";
import { WorkspacePanel } from "./workspace_panel.js";

// What Ningo knows: the documents it keeps, and the readings the feeds bring in. Reading is how
// knowledge comes in, not a place of its own, so both live in one workspace, each a view of it.
export function KnowledgeWorkspace({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const { tabs, panels, currentLabel } = useViewTabs(t("nav_library"), [
    { id: "documents", label: t("knowledge_view_documents") },
    { id: "sources", label: t("knowledge_view_sources") },
  ]);
  return (
    <WorkspacePanel trail={[t("nav_library"), currentLabel]} views={tabs}>
      {panels((id) => (id === "documents" ? <KnowledgeCard transport={transport} /> : <DigestCard transport={transport} />))}
    </WorkspacePanel>
  );
}
