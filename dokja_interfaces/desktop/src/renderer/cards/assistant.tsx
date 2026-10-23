import { useState } from "react";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { useOpenWorkspace } from "../sidebar.js";
import { useStatus } from "../status_context.js";
import { Composer, Transcript, useConversation } from "./assistant_conversation.js";
import { WorkspacePanel } from "./workspace_panel.js";

// The beginnings of a question, written as challenges. They fill the message for the owner to finish;
// they never send by themselves.
const STARTS: readonly MessageId[] = ["talk_start_against", "talk_start_missing", "talk_start_judge"];

// The conversation with Ningo, the front door. A new conversation is a new one for the orchestrator
// too: the panel is mounted again with a new id.
export function AssistantCard({ transport }: { transport: Transport }) {
  const [round, setRound] = useState(0);
  return <Talk key={round} transport={transport} onRestart={() => setRound((value) => value + 1)} />;
}

function Talk({ transport, onRestart }: { transport: Transport; onRestart: () => void }) {
  const t = useTranslate();
  const conversation = useConversation(transport);
  const { pulse } = useStatus();
  const open = useOpenWorkspace();
  const restart = <button type="button" onClick={onRestart}>{t("assistant_new_conversation")}</button>;
  const fill = (start: string) => {
    conversation.setDraft(`${start} `);
    conversation.field.current?.focus();
  };
  const dock = <Composer conversation={conversation} placeholder={t("assistant_placeholder")} hint={t("talk_hint")} />;
  return <WorkspacePanel trail={[t("nav_assistant")]} actions={restart} dock={dock}>
    <section className="talk" aria-label={t("assistant_title")}>
      {pulse === "lost" && <div className="note" data-tone="problem">
        <p>{t("talk_offline")}</p>
        <button type="button" onClick={() => open("health")}>{t("talk_open_system")}</button>
      </div>}
      {conversation.turns.length === 0 && <div className="talk-opening">
        <p className="voice">{t("talk_voice_opening")}</p>
        <p className="detail">{t("talk_next_step")}</p>
        <div className="talk-starts">
          {STARTS.map((id) => <button key={id} type="button" className="assistant-prompt" disabled={conversation.busy} onClick={() => fill(t(id))}>{t(id)}</button>)}
        </div>
      </div>}
      <Transcript conversation={conversation} />
    </section>
  </WorkspacePanel>;
}
