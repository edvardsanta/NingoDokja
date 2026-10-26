import { useState } from "react";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { useOpenWorkspace } from "../sidebar.js";
import { useDiscussion, type Reading } from "../discussion_context.js";
import { useStatus } from "../status_context.js";
import { Composer, READING_PROMPTS, Transcript, useConversation } from "./assistant_conversation.js";
import { WorkspacePanel } from "./workspace_panel.js";

// The beginnings of a question, written as challenges. They fill the message for the owner to finish;
// they never send by themselves.
const STARTS: readonly MessageId[] = ["talk_start_against", "talk_start_missing", "talk_start_judge"];

// The conversation with Ningo, the front door. A new conversation is a new one for the orchestrator
// too: the panel is mounted again with a new id, so a reply still on its way to the old one is
// dropped instead of landing here. A reading chosen in Readings starts a conversation about it.
export function AssistantCard({ transport }: { transport: Transport }) {
  const { reading, round, choose } = useDiscussion();
  // Talk's own restarts count apart from the discussions chosen elsewhere, so it restarts by itself.
  const [restarts, setRestarts] = useState(0);
  const restart = () => {
    if (reading) choose(undefined);
    setRestarts((value) => value + 1);
  };
  return <Talk key={`${round}-${restarts}`} transport={transport} reading={reading} onRestart={restart} />;
}

function Talk({ transport, reading, onRestart }: { transport: Transport; reading: Reading | undefined; onRestart: () => void }) {
  const t = useTranslate();
  const conversation = useConversation(transport, reading);
  const { pulse } = useStatus();
  const open = useOpenWorkspace();
  const restart = <button type="button" onClick={onRestart}>{t("assistant_new_conversation")}</button>;
  const fill = (start: string) => {
    conversation.setDraft(`${start} `);
    conversation.field.current?.focus();
  };
  const dock = <Composer conversation={conversation} placeholder={t("assistant_placeholder")} hint={t("talk_hint")} keyshortcuts="Control+K" />;
  return <WorkspacePanel trail={[t("nav_assistant")]} actions={restart} dock={dock}>
    <section className="talk" aria-label={t("assistant_title")}>
      {pulse === "lost" && <div className="note" data-tone="problem">
        <p>{t("talk_offline")}</p>
        <button type="button" onClick={() => open("health")}>{t("talk_open_system")}</button>
      </div>}
      {reading && <div className="talk-context">
        <p className="note">{t("assistant_reading_context", { title: reading.title, source: reading.source || t("assistant_unknown_source") })}</p>
        <button type="button" onClick={onRestart}>{t("assistant_close")}</button>
        <p className="detail">{t("assistant_excerpt_limit")}</p>
      </div>}
      {reading && conversation.turns.length === 0 && <div className="talk-opening">
        <p className="detail">{t("assistant_reading_intro")}</p>
        <div className="talk-starts">
          {READING_PROMPTS.map((id) => <button key={id} type="button" className="assistant-prompt" disabled={conversation.busy} onClick={() => void conversation.send(t(id))}>{t(id)}</button>)}
        </div>
      </div>}
      {!reading && conversation.turns.length === 0 && <div className="talk-opening">
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
