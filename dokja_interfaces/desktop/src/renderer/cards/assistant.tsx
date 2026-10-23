import { useState } from "react";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { AssistantConversation } from "./assistant_conversation.js";
import { WorkspacePanel } from "./workspace_panel.js";

export function AssistantCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const [conversation, setConversation] = useState(0);
  const restart = <button type="button" onClick={() => setConversation((value) => value + 1)}>{t("assistant_new_conversation")}</button>;
  return <WorkspacePanel trail={[t("nav_assistant")]} actions={restart}>
    <section className="wp-section" aria-labelledby="assistant-card-title">
      <h2 id="assistant-card-title">{t("assistant_title")}</h2>
      <p className="detail">{t("assistant_intro_long")}</p>
      <AssistantConversation key={conversation} transport={transport} />
    </section>
  </WorkspacePanel>;
}
