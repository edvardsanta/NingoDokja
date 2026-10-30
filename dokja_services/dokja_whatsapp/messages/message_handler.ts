import type { WhatsAppConfig } from "../config.js";
import {
  buildWhatsAppBridgeRequest,
  dispatchToOrchestratorResponse,
  type IncomingDocument,
  type ReplyMedia,
  type WhatsAppContentReply,
} from "../transport/bridge.js";
import { whatsappLocale, whatsappMessage } from "./locales.js";
import { RecentKeys } from "./recent_keys.js";

// A WhatsApp text message may hold about 64 KB. A chat answer that long is unreadable anyway.
export const maxReplyLength = 8000;
export const failureReply = whatsappMessage(undefined, "failure");
// Every message Ningo writes ends with its signature: on a personal number the replies come from the
// owner's own account, so the chat has to tell Ningo's words from the owner's. WhatsApp shows
// _text_ in italics. What the owner sends from the desktop is the owner's and is not signed.
export const signature = "\n\n" + whatsappMessage(undefined, "signature");

// The reply as it is sent: cut so that the whole message, signature included, stays within bounds.
export function signed(text: string, locale?: string): string {
  const signature = "\n\n" + whatsappMessage(locale, "signature");
  return `${text.trimEnd().slice(0, maxReplyLength - signature.length)}${signature}`;
}
// Messages that arrive late (the bot was offline or reconnecting) are not answered.
export const maxMessageAgeMs = 5 * 60_000;
// "Typing" is shown only when the answer is slow. An interface that is switched off in the desktop
// is skipped by the orchestrator at once, and must not make the chat flash "typing" first.
export const typingDelayMs = 1000;

export type WhatsAppMessageConfig = Pick<
  WhatsAppConfig,
  "locale" | "orchestratorEndpoint" | "orchestratorTimeoutMs" | "selfChat" | "allowedNumbers" | "allowedGroups"
>;

export type WhatsAppMessageContext = {
  // The message text with the bot's own mention already removed.
  text: string;
  document?: IncomingDocument;
  downloadDocument?: () => Promise<Buffer>;
  replyMedia?: (media: ReplyMedia, caption: string) => Promise<unknown>;
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
  dispatch?: (
    endpoint: string,
    timeoutMs: number,
    body: ReturnType<typeof buildWhatsAppBridgeRequest>,
  ) => Promise<string | WhatsAppContentReply>;
  recent?: RecentKeys;
  now?: () => number;
  log?: (...args: unknown[]) => void;
  error?: (...args: unknown[]) => void;
};

export function createWhatsAppMessageHandler(
  config: WhatsAppMessageConfig,
  deps: Dependencies = {},
): WhatsAppMessageHandler {
  const dispatch = deps.dispatch ?? dispatchToOrchestratorResponse;
  const locale = whatsappLocale(config.locale);
  const recent = deps.recent ?? new RecentKeys();
  const now = deps.now ?? Date.now;
  // A group the bot is not allowed in is reported once, so its id can be copied into
  // DOKJA_WHATSAPP_ALLOWED_GROUPS without logging every message of a busy chat.
  const reportedGroups = new Set<string>();

  return async (received) => {
    const context = nameCall(received, config.allowedGroups);
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
    let response: WhatsAppContentReply;
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
      const body = buildWhatsAppBridgeRequest({
        content: context.text,
        document: context.document,
        locale,
        userId: context.senderId,
        chatId: context.chatId,
        messageId: context.messageId,
        chatKind: context.chatKind,
        senderNumbers: context.senderNumbers,
        isSelfChat: context.isSelfChat,
        mentionsBot: context.mentionsBot,
      });
      const normalize = (value: string | WhatsAppContentReply): WhatsAppContentReply =>
        typeof value === "string" ? { reply: value } : value;
      response = normalize(await dispatch(config.orchestratorEndpoint, config.orchestratorTimeoutMs, body));
      // Authorization and format/size checks happen on the metadata-only turn.
      // Denied input returns an empty response, so its file is never downloaded.
      if (response.document_required && context.document) {
        let bytes: Buffer | undefined;
        try {
          if (!context.downloadDocument) throw new Error("document download unavailable");
          bytes = await context.downloadDocument();
          if (!bytes.length) throw new Error("empty document");
          if (bytes.length > 10 * 1024 * 1024) {
            throw Object.assign(new Error("document too large"), { code: "document_too_large" });
          }
        } catch (error) {
          const reason = (error as { code?: string })?.code === "document_too_large"
            ? "document_too_large" : "document_download_failed";
          response = { reply: "", content_error: reason };
          bytes = undefined;
        }
        if (bytes) {
          body.payload.document = { ...context.document, size: bytes.length, data_b64: bytes.toString("base64") };
          response = normalize(await dispatch(config.orchestratorEndpoint, config.orchestratorTimeoutMs, body));
        }
      }
      reply = response.content_error ? whatsappMessage(locale, response.content_error) : response.reply;
      if (response.summary_partial) reply = whatsappMessage(locale, "summary_partial") + "\n\n" + reply;

    } catch (error) {
      deps.error?.(JSON.stringify({
        message: "dispatch failed",
        messageId: context.messageId,
        error: error instanceof Error ? error.message : String(error),
      }));
      await stopTyping();
      await context.reply(signed(whatsappMessage(locale, "failure"), locale));
      return;
    }

    await stopTyping();
    if (response.attachments?.length) {
      try {
        if (!context.replyMedia) throw new Error("media delivery unavailable");
        const media = validatedReplyMedia(response.attachments);
        const signatureLength = ("\n\n" + whatsappMessage(locale, "signature")).length;
        const firstCaption = reply.slice(0, 1000 - signatureLength);
        for (let index = 0; index < media.length; index++) {
          await context.replyMedia(media[index]!, signed(index === 0 ? firstCaption : "", locale));
        }
        const textLimit = maxReplyLength - signatureLength;
        for (let offset = firstCaption.length; offset < reply.length; offset += textLimit) {
          await context.reply(signed(reply.slice(offset, offset + textLimit), locale));
        }
      } catch {
        await context.reply(signed(whatsappMessage(locale, "media_delivery_failed"), locale));
      }
      return;
    }
    if (!reply.trim()) {
      deps.log?.(JSON.stringify({ message: "completed", messageId: context.messageId, replyLength: 0 }));
      return;
    }
    await context.reply(signed(reply, locale));
    deps.log?.(JSON.stringify({ message: "replied", messageId: context.messageId, replyLength: reply.length }));
  };
}

// In a group the owner allowed, anyone calls Ningo by name: a message that starts with "Ningo," or
// "/ningo" has the word taken off and counts as a mention. On a personal number the bot is the owner's
// own account, so the owner cannot mention it and tagging the account tags the owner; a name keeps
// the two apart. The owner's own call is answered too, while everything else the owner writes in a
// group stays ignored. Answering one of Ningo's messages still counts as calling it.
export const nameTrigger = /^\s*(?:\/ningo\b|ningo\s*,)\s*/i;

export function nameCall(context: WhatsAppMessageContext, allowedGroups: readonly string[]): WhatsAppMessageContext {
  if (context.sentByBot || context.chatKind !== "group" || !allowedGroups.includes(context.chatId) || !nameTrigger.test(context.text)) {
    return context;
  }
  return { ...context, text: context.text.replace(nameTrigger, ""), fromMe: false, mentionsBot: true };
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

  if (!context.text.trim() && !context.document) {
    return "empty";
  }
  return null;
}

export function validatedReplyMedia(rows: ReplyMedia[]): ReplyMedia[] {
  if (!Array.isArray(rows) || rows.length === 0 || rows.length > 10) throw new Error("invalid media count");
  let total = 0;
  for (const row of rows) {
    if (!row || !["image/jpeg", "image/png", "image/webp", "video/mp4"].includes(row.content_type)
      || typeof row.data_b64 !== "string" || row.data_b64.length > Math.ceil(8 * 1024 * 1024 / 3) * 4) {
      throw new Error("invalid media");
    }
    const bytes = Buffer.from(row.data_b64, "base64");
    total += bytes.length;
    if (!bytes.length || bytes.length > 8 * 1024 * 1024 || bytes.toString("base64") !== row.data_b64
      || total > 16 * 1024 * 1024) throw new Error("invalid media bytes");
  }
  return rows;
}
