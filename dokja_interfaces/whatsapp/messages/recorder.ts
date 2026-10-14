import { numberAllowed, type Conversation } from "../config.js";
import type { HistoryStore, Who } from "../history/store.js";
import type { WhatsAppMessageContext } from "./message_handler.js";

// The alias of the conversation a message belongs to, or undefined when the owner did not name it.
// Only a named conversation is ever recorded: everything else the account receives is dropped here.
export function aliasFor(conversations: Conversation[], context: WhatsAppMessageContext): string | undefined {
  for (const conversation of conversations) {
    if (conversation.group && context.chatKind === "group" && context.chatId === conversation.group) {
      return conversation.alias;
    }
    if (
      conversation.number &&
      context.chatKind === "direct" &&
      numberAllowed([conversation.number], context.senderNumbers)
    ) {
      return conversation.alias;
    }
  }
  return undefined;
}

export type Recorder = {
  // A message that arrived (any, whether or not it gets an answer).
  inbound(context: WhatsAppMessageContext): Promise<void>;
  // A message the bot sent into the conversation of this context.
  outbound(context: WhatsAppMessageContext, text: string, id: string | undefined): Promise<void>;
};

export function createRecorder(
  conversations: Conversation[],
  store: HistoryStore,
  options: { now?: () => number; error?: (...args: unknown[]) => void } = {},
): Recorder {
  const now = options.now ?? Date.now;
  const record = async (alias: string, entry: { id: string; at: number; who: Who; text: string }) => {
    try {
      await store.append(alias, entry);
    } catch (error) {
      // A history that cannot be written never stops the bot from answering.
      options.error?.(JSON.stringify({
        message: "could not record a message",
        error: error instanceof Error ? error.message : String(error),
      }));
    }
  };

  return {
    async inbound(context) {
      const alias = aliasFor(conversations, context);
      if (!alias || !context.text.trim()) {
        return;
      }
      const who: Who = context.sentByBot ? "bot" : context.fromMe ? "owner" : context.chatKind === "group" ? "member" : "contact";
      await record(alias, { id: context.messageId, at: context.timestampMs, who, text: context.text });
    },
    async outbound(context, text, id) {
      const alias = aliasFor(conversations, context);
      if (!alias) {
        return;
      }
      await record(alias, { id: id ?? `out-${now()}-${Math.random().toString(36).slice(2, 8)}`, at: now(), who: "bot", text });
    },
  };
}
