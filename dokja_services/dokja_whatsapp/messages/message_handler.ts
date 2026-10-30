import type { WhatsAppConfig } from "../config.js";
import { buildWhatsAppBridgeRequest, dispatchToOrchestrator } from "../transport/bridge.js";
import { RecentKeys } from "./recent_keys.js";

// A WhatsApp text message may hold about 64 KB. A chat answer that long is unreadable anyway.
export const maxReplyLength = 8000;
export const failureReply = "I could not reach the orchestrator right now. Try again in a moment.";
// Every message Ningo writes ends with its signature: on a personal number the replies come from the
// owner's own account, so the chat has to tell Ningo's words from the owner's. WhatsApp shows
// _text_ in italics. What the owner sends from the desktop is the owner's and is not signed.
export const signature = "\n\nAssinado _Ningo Dokja_";

// The reply as it is sent: cut so that the whole message, signature included, stays within bounds.
export function signed(text: string): string {
  return `${text.trimEnd().slice(0, maxReplyLength - signature.length)}${signature}`;
}
// Messages that arrive late (the bot was offline or reconnecting) are not answered.
export const maxMessageAgeMs = 5 * 60_000;
// "Typing" is shown only when the answer is slow. An interface that is switched off in the desktop
// is skipped by the orchestrator at once, and must not make the chat flash "typing" first.
export const typingDelayMs = 1000;

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
  typingDelayMs?: number;
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

  return async (received) => {
    const context = ownerCall(received, config.allowedGroups);
    const ignored = ignoreReason(context, now());
    if (ignored) {
      // Ignored messages are most of what a personal account receives, so they are not logged
      // one by one, and a phone number never reaches the log.
      return;
    }
    if (context.chatKind === "group" && !config.allowedGroups.includes(context.chatId) && !reportedGroups.has(context.chatId)) {
      reportedGroups.add(context.chatId);
      deps.log?.(JSON.stringify({ message: "ignored", reason: "policy", conversation: context.chatId }));
    }
    if (recent.seen(`${context.chatId}:${context.messageId}`)) {
      deps.log?.(JSON.stringify({ message: "ignored", reason: "duplicate", messageId: context.messageId }));
      return;
    }

    let reply: string;
    let typing = false;
    const typingTimer = setTimeout(() => {
      typing = true;
      void context.typing(true).catch(() => undefined);
    }, deps.typingDelayMs ?? typingDelayMs);
    const stopTyping = async () => {
      clearTimeout(typingTimer);
      if (typing) {
        await context.typing(false).catch(() => undefined);
      }
    };
    try {
      reply = await dispatch(
        config.orchestratorEndpoint,
        config.orchestratorTimeoutMs,
        buildWhatsAppBridgeRequest({
          content: context.text,
          userId: context.senderId,
          chatId: context.chatId,
          messageId: context.messageId,
          chatKind: context.chatKind,
          senderNumbers: context.senderNumbers,
          isSelfChat: context.isSelfChat,
          mentionsBot: context.mentionsBot,
        }),
      );
    } catch (error) {
      deps.error?.(JSON.stringify({
        message: "dispatch failed",
        messageId: context.messageId,
        error: error instanceof Error ? error.message : String(error),
      }));
      await stopTyping();
      await context.reply(signed(failureReply));
      return;
    }

    await stopTyping();
    if (!reply.trim()) {
      deps.log?.(JSON.stringify({ message: "completed", messageId: context.messageId, replyLength: 0 }));
      return;
    }
    await context.reply(signed(reply));
    deps.log?.(JSON.stringify({ message: "replied", messageId: context.messageId, replyLength: reply.length }));
  };
}

// On a personal number the owner cannot mention the bot, which is the owner's own account, so what
// the owner writes in a group is ignored. In a group the owner allowed, a message that starts with
// "Ningo," or "/ningo" calls the bot instead: the word is taken off and the message counts as a
// mention. Everything else the owner writes in a group stays ignored.
export const ownerTrigger = /^\s*(?:\/ningo\b|ningo\s*,)\s*/i;

export function ownerCall(context: WhatsAppMessageContext, allowedGroups: readonly string[]): WhatsAppMessageContext {
  if (!context.fromMe || context.sentByBot || context.chatKind !== "group" || !allowedGroups.includes(context.chatId) || !ownerTrigger.test(context.text)) {
    return context;
  }
  return { ...context, text: context.text.replace(ownerTrigger, ""), fromMe: false, mentionsBot: true };
}

// Returns why a message is not for the bot, or null when it is.
function ignoreReason(context: WhatsAppMessageContext, now: number): string | null {
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

  if (!context.text.trim()) {
    return "empty";
  }
  return null;
}
