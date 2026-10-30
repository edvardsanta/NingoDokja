import { useEffect, useState, type FormEvent, type KeyboardEvent } from "react";
import type { Transport, TransportError } from "../../shared/transport.js";
import type { ArchivedConversation, ConversationList } from "../../shared/replies.js";
import { isRecord } from "../../shared/records.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import { Problem } from "./card_frame.js";
import { Drawer } from "./drawer.js";

const PAGE = 20;

function page(result: unknown): ConversationList | undefined {
  return isRecord(result) && Array.isArray(result.conversations) ? (result as ConversationList) : undefined;
}

// The owner's conversations from the archive, the most recently changed first, a page at a time.
// Choosing one hands it to Talk; the list itself changes nothing.
// Each kept conversation can be renamed in place or deleted after the shell's own question. Both name
// the version the list read, so a change made elsewhere meanwhile is refused and the list read again.
export function ConversationsDrawer({ transport, current, onOpen, onDeleted, onClose }: {
  transport: Transport;
  current: string;
  onOpen: (conversation: ArchivedConversation) => void;
  onDeleted: (id: string) => void;
  onClose: () => void;
}) {
  const t = useTranslate();
  const { ago } = useTimes();
  const [items, setItems] = useState<ArchivedConversation[]>([]);
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<TransportError>();
  const [renaming, setRenaming] = useState<{ id: string; title: string }>();
  const [working, setWorking] = useState(false);
  const [problem, setProblem] = useState<TransportError>();
  const [changed, setChanged] = useState(false);

  const load = async (cursor = "") => {
    setLoading(true); setError(undefined);
    const answer = await transport.request("conversation.list", { limit: PAGE, ...(cursor ? { cursor } : {}) });
    const listed = answer.ok ? page(answer.result) : undefined;
    if (listed) {
      setItems((previous) => (cursor ? [...previous, ...listed.conversations] : listed.conversations));
      setNext(listed.next);
    } else setError(answer.ok ? { code: "unexpected", message: "The conversations could not be read." } : answer.error);
    setLoading(false);
  };
  useEffect(() => { void load(); }, []);

  // A refusal because the conversation changed elsewhere reads the list again; any other is shown.
  const refused = async (failure: TransportError) => {
    if (/changed; read it again/.test(failure.message)) { setChanged(true); await load(); }
    else setProblem(failure);
  };

  const rename = async (event: FormEvent, item: ArchivedConversation) => {
    event.preventDefault();
    if (!renaming || working) return;
    setWorking(true); setProblem(undefined); setChanged(false);
    const answer = await transport.request("conversation.rename", { conversation_id: item.id, revision: item.revision, title: renaming.title });
    setWorking(false);
    const renamed = answer.ok && isRecord(answer.result) && isRecord(answer.result.conversation) ? (answer.result.conversation as ArchivedConversation) : undefined;
    if (renamed) {
      setItems((previous) => previous.map((each) => (each.id === item.id ? renamed : each)));
      setRenaming(undefined);
    } else if (!answer.ok) { setRenaming(undefined); await refused(answer.error); }
  };

  const remove = async (item: ArchivedConversation) => {
    if (working) return;
    setWorking(true); setProblem(undefined); setChanged(false);
    const answer = await transport.request("conversation.delete", { conversation_id: item.id, revision: item.revision });
    setWorking(false);
    if (answer.ok) {
      setItems((previous) => previous.filter((each) => each.id !== item.id));
      onDeleted(item.id);
    } else if (answer.error.code !== "cancelled") await refused(answer.error);
  };

  // Escape leaves the field, not the drawer.
  const keepEscape = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Escape") { event.stopPropagation(); setRenaming(undefined); }
  };

  return <Drawer title={t("conversations_title")} onClose={onClose}>
    {error && <><Problem error={error}/><button type="button" onClick={() => void load()}>{t("assistant_reload")}</button></>}
    {!error && !loading && items.length === 0 && <p className="detail">{t("conversations_empty")}</p>}
    {changed && <p role="status">{t("conversation_changed")}</p>}
    {problem && <Problem error={problem}/>}
    {items.length > 0 && <ul className="conversation-list">{items.map((item) => {
      const title = item.title || t("conversation_untitled");
      return <li key={item.id}>
        {renaming?.id === item.id
          ? <form className="conversation-rename" onSubmit={(event) => void rename(event, item)}>
            <label htmlFor={`rename-${item.id}`}>{t("conversation_title_label")}</label>
            <input id={`rename-${item.id}`} value={renaming.title} maxLength={120} autoFocus onKeyDown={keepEscape}
              onChange={(event) => setRenaming({ id: item.id, title: event.target.value })}/>
            <button type="submit" className="primary" disabled={working}>{t("conversation_save")}</button>
            <button type="button" onClick={() => setRenaming(undefined)}>{t("conversation_cancel")}</button>
          </form>
          : <button type="button" className="row-title" aria-current={item.id === current ? "true" : undefined} onClick={() => onOpen(item)}>
            <strong>{title}</strong>
          </button>}
        <span className="detail">{t(item.kind === "reading" ? "conversation_meta_reading" : "conversation_meta", { turns: String(item.turns), ago: ago(item.updatedAt) })}</span>
        {renaming?.id !== item.id && <span className="conversation-actions">
          <button type="button" disabled={working} aria-label={t("conversation_rename_named", { title })} onClick={() => setRenaming({ id: item.id, title: item.title })}>{t("conversation_rename")}</button>
          <button type="button" disabled={working} aria-label={t("conversation_delete_named", { title })} onClick={() => void remove(item)}>{t("conversation_delete")}</button>
        </span>}
      </li>;
    })}</ul>}
    {loading && <p role="status">{t("assistant_opening_list")}</p>}
    {!loading && !error && next && <button type="button" onClick={() => void load(next)}>{t("conversations_more")}</button>}
  </Drawer>;
}

// The conversation Talk reopens when the desktop starts: the most recently changed general one that
// has something in it. A reading's discussion is reopened only from the list, with its reading.
export async function latestConversation(transport: Transport): Promise<ArchivedConversation | undefined> {
  const answer = await transport.request("conversation.list", { limit: PAGE }).catch(() => undefined);
  const listed = answer?.ok ? page(answer.result) : undefined;
  return listed?.conversations.find((item) => item.kind === "general" && item.turns > 0);
}
