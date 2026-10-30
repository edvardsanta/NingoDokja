import { useEffect, useState } from "react";
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
export function ConversationsDrawer({ transport, current, onOpen, onClose }: {
  transport: Transport;
  current: string;
  onOpen: (conversation: ArchivedConversation) => void;
  onClose: () => void;
}) {
  const t = useTranslate();
  const { ago } = useTimes();
  const [items, setItems] = useState<ArchivedConversation[]>([]);
  const [next, setNext] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<TransportError>();

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

  return <Drawer title={t("conversations_title")} onClose={onClose}>
    {error && <><Problem error={error}/><button type="button" onClick={() => void load()}>{t("assistant_reload")}</button></>}
    {!error && !loading && items.length === 0 && <p className="detail">{t("conversations_empty")}</p>}
    {items.length > 0 && <ul className="conversation-list">{items.map((item) => <li key={item.id}>
      <button type="button" className="row-title" aria-current={item.id === current ? "true" : undefined} onClick={() => onOpen(item)}>
        <strong>{item.title || t("conversation_untitled")}</strong>
      </button>
      <span className="detail">{t(item.kind === "reading" ? "conversation_meta_reading" : "conversation_meta", { turns: String(item.turns), ago: ago(item.updatedAt) })}</span>
    </li>)}</ul>}
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
