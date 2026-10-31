import type { ActionType } from "../shared/actions.js";
import type { Locale } from "../shared/locale.js";
import { createTranslator, type MessageId } from "../renderer/i18n/i18n.js";

// The question the shell asks, in a window of its own, before an action that cannot be undone. It is
// asked by the main process, not by the page: a page that was tricked into asking cannot answer for
// the person, and cannot change what the window says. The window names what is about to be
// deleted by its id, which is what the shell knows, not by a title the page could make up.
export type Question = { message: string; detail: string; accept: string; cancel: string };

// Asks the person, in a window the shell owns, whether to go on. True means yes.
export type Confirm = (type: ActionType, wire: Record<string, unknown>) => Promise<boolean>;

// Shows one question and answers whether the person accepted it.
export type ShowQuestion = (question: Question) => Promise<boolean>;

// One question at a time: while one is open, another request to ask is a no, so a page that was
// tricked into asking again and again cannot pile windows on the person.
export function createConfirmer(locale: Locale, show: ShowQuestion): Confirm {
  let open = false;
  return async (type, wire) => {
    const question = questionFor(locale, type, wire);
    if (!question || open) return false;
    open = true;
    try {
      return await show(question);
    } finally {
      open = false;
    }
  };
}

export function questionFor(locale: Locale, type: ActionType, wire: Record<string, unknown>): Question | undefined {
  const t = createTranslator(locale);
  const ask = (message: MessageId, detail: string, accept: MessageId): Question =>
    ({ message: t(message), detail, accept: t(accept), cancel: t("dialog_cancel") });
  switch (type) {
    case "communications.send": {
      const channel = String((wire.channel_ids as string[])[0]);
      if (channel.startsWith("whatsapp:")) {
        return ask("dialog_whatsapp_send", t("dialog_whatsapp_send_detail", { conversation: channel.slice("whatsapp:".length), content: String(wire.content ?? "") }), "dialog_send");
      }
      const warning = wire.force_nsfw === true ? `\n\n${t("dialog_discord_force_warning")}` : "";
      return ask("dialog_discord_send", `${t("dialog_discord_send_detail", { channel, content: String(wire.content ?? ""), attachment: String(wire.attachment_url ?? "") })}${warning}`, "dialog_send");
    }
    case "communications.link.reset":
      return ask("dialog_unlink", t("dialog_unlink_detail"), "dialog_unlink_accept");
    case "memory.resolve":
      return ask("dialog_feedback", t("dialog_feedback_detail", { ref: String(wire.ref), observed: String(wire.observed ?? wire.outcome) }), "dialog_feedback_accept");
    case "runtime.models.pull":
      return ask("dialog_model_pull", t("dialog_model_pull_detail", { model: String(wire.model) }), "dialog_model_pull_accept");
    case "runtime.models.remove":
      return ask("dialog_model_remove", t("dialog_model_remove_detail", { model: String(wire.model) }), "dialog_model_remove_accept");
    case "conversation.delete":
      return ask("dialog_conversation_delete", t("dialog_conversation_delete_detail", { id: String(wire.conversation_id) }), "dialog_delete");
    case "conversation.fact_delete":
      return ask("dialog_fact_delete", t("dialog_fact_delete_detail"), "dialog_fact_delete_accept");
    case "knowledge.delete":
      return ask("dialog_document_delete", t("dialog_document_delete_detail", { id: String(wire.source_id ?? "") }), "dialog_delete");
    default:
      return undefined;
  }
}
