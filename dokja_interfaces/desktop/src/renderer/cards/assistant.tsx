import { useState } from "react";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { AssistantConversation } from "./assistant_conversation.js";

export function AssistantCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const [conversation, setConversation] = useState(0);
  return <section className="card" aria-labelledby="assistant-card-title">
    <span className="card-kind">{t("nav_assistant")}</span>
    <h2 id="assistant-card-title">{t("assistant_title")}</h2>
    <p className="detail">{t("assistant_intro_long")}</p>
    <AssistantConversation key={conversation} transport={transport} />
    <footer><button type="button" onClick={() => setConversation((value) => value + 1)}>{t("assistant_new_conversation")}</button></footer>
  </section>;
}
