import { numberAllowed, type WhatsAppConfig } from "../config.js";
import { buildWhatsAppBridgeRequest, dispatchToOrchestrator } from "../transport/bridge.js";
import { RecentKeys } from "./recent_keys.js";

// A WhatsApp text message may hold about 64 KB. A chat answer that long is unreadable anyway.
export const maxReplyLength = 8000;
export const failureReply = "I could not reach the orchestrator right now. Try again in a moment.";
// Messages that arrive late (the bot was offline or reconnecting) are not answered.
export const maxMessageAgeMs = 5 * 60_000;

export type WhatsAppMessageConfig = Pick<
  WhatsAppConfig,
  "orchestratorEndpoint" | "orchestratorTimeoutMs" | "selfChat" | "allowedNumbers" | "allowedGroups"
>;

export type WhatsAppMessageContext = {
  // The message text with the bot's own mention already removed.
  text: string;
  messageId: string;
  chatId: string;
  // "other" covers status updates, broadcast lists and channels, which are never answered.
  chatKind: "direct" | "group" | "other";
  // Who wrote it: the participant in a group, the other side in a direct chat.
  senderId: string;
  // Phone numbers (digits) known for the sender. Empty when only an anonymous id is known.
  senderNumbers: string[];
  fromMe: boolean;
  // The chat is the account's own "message yourself" chat.
  isSelfChat: boolean;
  // The message was sent by this bot, so it is the echo of a reply.
  sentByBot: boolean;
  // The bot is mentioned, or the message replies to one of the bot's own messages.
  mentionsBot: boolean;
  timestampMs: number;
  reply: (text: string) => Promise<unknown>;
  typing: (active: boolean) => Promise<void>;
};

export type WhatsAppMessageHandler = (context: WhatsAppMessageContext) => Promise<void>;

type Dependencies = {
  dispatch?: typeof dispatchToOrchestrator;
  recent?: RecentKeys;
  now?: () => number;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

export function createWhatsAppMessageHandler(
  config: WhatsAppMessageConfig,
  deps: Dependencies = {},
): WhatsAppMessageHandler {
  const dispatch = deps.dispatch ?? dispatchToOrchestrator;
  const recent = deps.recent ?? new RecentKeys();
  const now = deps.now ?? Date.now;
  // A group the bot is not allowed in is reported once, so its id can be copied into
  // DOKJA_WHATSAPP_ALLOWED_GROUPS without logging every message of a busy chat.
  const reportedGroups = new Set<string>();

  return async (context) => {
    const ignored = ignoreReason(context, config, now());
    if (ignored) {
      // Ignored messages are most of what a personal account receives, so they are not logged
      // one by one, and a phone number never reaches the log.
      if (ignored === "policy" && context.chatKind === "group" && !reportedGroups.has(context.chatId)) {
        reportedGroups.add(context.chatId);
        deps.log?.(JSON.stringify({ message: "ignored", reason: "policy", conversation: context.chatId }));
      }
      return;
    }
    if (recent.seen(`${context.chatId}:${context.messageId}`)) {
      deps.log?.(JSON.stringify({ message: "ignored", reason: "duplicate", messageId: context.messageId }));
      return;
    }

    deps.log?.(JSON.stringify({
      message: "dispatch",
      messageId: context.messageId,
      chatKind: context.isSelfChat ? "self" : context.chatKind,
      contentLength: context.text.length,
    }));

    let reply: string;
    await context.typing(true).catch(() => undefined);
    try {
      reply = await dispatch(
        config.orchestratorEndpoint,
        config.orchestratorTimeoutMs,
        buildWhatsAppBridgeRequest({
          content: context.text,
          userId: context.senderId,
          chatId: context.chatId,
          messageId: context.messageId,
        }),
      );
    } catch (error) {
      deps.error?.(JSON.stringify({
        message: "dispatch failed",
        messageId: context.messageId,
        error: error instanceof Error ? error.message : String(error),
      }));
      await context.typing(false).catch(() => undefined);
      await context.reply(failureReply);
      return;
    }

    await context.typing(false).catch(() => undefined);
    if (!reply.trim()) {
      deps.log?.(JSON.stringify({ message: "completed", messageId: context.messageId, replyLength: 0 }));
      return;
    }
    await context.reply(reply.slice(0, maxReplyLength));
    deps.log?.(JSON.stringify({ message: "replied", messageId: context.messageId, replyLength: reply.length }));
  };
}

// Returns why a message is not for the bot, or null when it is.
function ignoreReason(context: WhatsAppMessageContext, config: WhatsAppMessageConfig, now: number): string | null {
  if (context.chatKind === "other") {
    return "not-chat";
  }
  // The echo of a reply, or something the owner typed in another chat.
  if (context.sentByBot || (context.fromMe && !context.isSelfChat)) {
    return "own";
  }
  if (now - context.timestampMs > maxMessageAgeMs) {
    return "stale";
  }

  if (context.isSelfChat) {
    if (!config.selfChat) {
      return "policy";
    }
  } else if (context.chatKind === "direct") {
    if (!numberAllowed(config.allowedNumbers, context.senderNumbers)) {
      return "policy";
    }
  } else {
    if (!config.allowedGroups.includes(context.chatId)) {
      return "policy";
    }
    if (!context.mentionsBot) {
      return "not-mentioned";
    }
  }

  if (!context.text.trim()) {
    return "empty";
  }
  return null;
}
