import { useState } from "react";
import type { ArchivedConversation } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId, Translate } from "../i18n/i18n.js";
import { useOpenWorkspace } from "../sidebar.js";
import { useDiscussion, type Reading } from "../discussion_context.js";
import { useStatus } from "../status_context.js";
import { Composer, READING_PROMPTS, Transcript, useConversation } from "./assistant_conversation.js";
import { ConversationsDrawer } from "./conversation_list.js";
import { WorkspacePanel } from "./workspace_panel.js";

// The beginnings of a question, written as challenges. They fill the message for the owner to finish;
// they never send by themselves.
const STARTS: readonly MessageId[] = ["talk_start_against", "talk_start_missing", "talk_start_judge"];

// What a reading's discussion opens with: the questions its own source wrote from its text when it
// gave any, otherwise the generic ones that fit any reading.
function readingPrompts(reading: Reading, t: Translate): readonly string[] {
  return reading.prompts && reading.prompts.length > 0 ? reading.prompts : READING_PROMPTS.map((id) => t(id));
}

// The conversation with Ningo, the front door, kept in the orchestrator's archive. When the desktop
// starts, Talk reopens the latest general conversation; the list reopens any other. Each conversation
// is its own mounting of the panel, so a reply still on its way to another one is dropped instead of
// landing here. A reading chosen in Readings starts a conversation about it.
export function AssistantCard({ transport }: { transport: Transport }) {
  const { reading, round, choose } = useDiscussion();
  // Talk's own restarts count apart from the discussions chosen elsewhere, so it restarts by itself.
  const [restarts, setRestarts] = useState(0);
  const [opened, setOpened] = useState<ArchivedConversation>();
  // The conversation the list was opened from, while it is open.
  const [listing, setListing] = useState<string>();
  const restart = () => {
    if (reading) choose(undefined);
    setOpened(undefined);
    setRestarts((value) => value + 1);
  };
  const open = (conversation: ArchivedConversation) => {
    setListing(undefined);
    if (reading) choose(undefined);
    setOpened(conversation);
    setRestarts((value) => value + 1);
  };
  // Only the first Talk, before anything was chosen, continues the latest conversation.
  const restore = restarts === 0 && round === 0;
  const shown = reading ? undefined : opened;
  return <>
    <Talk key={`${round}-${restarts}`} transport={transport} reading={reading} opened={shown} restore={restore} onRestart={restart} onList={setListing} />
    {listing !== undefined && <ConversationsDrawer transport={transport} current={listing} onOpen={open} onDeleted={(id) => { if (id === listing) { setListing(""); restart(); } }} onClose={() => setListing(undefined)} />}
  </>;
}

function Talk({ transport, reading, opened, restore, onRestart, onList }: { transport: Transport; reading: Reading | undefined; opened: ArchivedConversation | undefined; restore: boolean; onRestart: () => void; onList: (current: string) => void }) {
  const t = useTranslate();
  const conversation = useConversation(transport, reading, opened, restore);
  const { pulse } = useStatus();
  const open = useOpenWorkspace();
  const restart = <>
    <button type="button" onClick={() => onList(conversation.id)}>{t("conversations_open")}</button>
    <button type="button" onClick={onRestart}>{t("assistant_new_conversation")}</button>
  </>;
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
      {conversation.opened?.kind === "reading" && <p className="note">{t("assistant_reading_reopened")}</p>}
      {reading && conversation.turns.length === 0 && <div className="talk-opening">
        <p className="detail">{t("assistant_reading_intro")}</p>
        <div className="talk-starts">
          {readingPrompts(reading, t).map((prompt) => <button key={prompt} type="button" className="assistant-prompt" disabled={conversation.busy} onClick={() => void conversation.send(prompt)}>{prompt}</button>)}
        </div>
      </div>}
      {!reading && !conversation.opened && !conversation.loading && conversation.turns.length === 0 && <div className="talk-opening">
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
