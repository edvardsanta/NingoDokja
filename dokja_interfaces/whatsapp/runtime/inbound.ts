import type { WhatsAppMessageContext } from "../messages/message_handler.js";
import type { RecentKeys } from "../messages/recent_keys.js";

// The part of a WhatsApp message this adapter reads. `remoteJidAlt` and `participantAlt` carry
// the other form of the id (phone number or anonymous id) when the message arrives with one.
export type InboundMessage = {
  key: {
    id?: string | null;
    remoteJid?: string | null;
    remoteJidAlt?: string | null;
    fromMe?: boolean | null;
    participant?: string | null;
    participantAlt?: string | null;
  };
  message?: InboundContent | null;
  messageTimestamp?: number | string | { toNumber(): number } | null;
};

type InboundContent = {
  conversation?: string | null;
  extendedTextMessage?: { text?: string | null; contextInfo?: InboundContextInfo | null } | null;
  [wrapper: string]: unknown;
};

type InboundContextInfo = {
  mentionedJid?: string[] | null;
  stanzaId?: string | null;
};

export type InboundDependencies = {
  // The ids of the linked account itself (phone-number and anonymous forms).
  ownJids: () => string[];
  // Ids of the messages this bot sent, to recognise their echo and replies to them.
  sent: RecentKeys;
  send: (chatId: string, text: string, options: { messageId: string; quoted?: InboundMessage }) => Promise<unknown>;
  presence: (chatId: string, active: boolean) => Promise<void>;
  // Looks up the phone number of an anonymous id; null when it is not known.
  resolveNumber?: (jid: string) => Promise<string | null>;
  newMessageId: () => string;
  now?: () => number;
};

const phoneServer = "s.whatsapp.net";
const anonymousServer = "lid";
const groupServer = "g.us";

// Wrappers that hold the real message, e.g. when the chat has disappearing messages on.
const wrappers = ["ephemeralMessage", "viewOnceMessage", "viewOnceMessageV2", "documentWithCaptionMessage"];

// "<user>[:<device>]@<server>" -> the account part and the server.
export function parseJid(jid: string | null | undefined): { user: string; server: string } | null {
  if (!jid) {
    return null;
  }
  const at = jid.lastIndexOf("@");
  if (at <= 0) {
    return null;
  }
  return { user: jid.slice(0, at).split(":")[0], server: jid.slice(at + 1) };
}

// The id without the device part, so one account is always spelled the same way.
export function accountJid(jid: string | null | undefined): string {
  const parsed = parseJid(jid);
  return parsed ? `${parsed.user}@${parsed.server}` : "";
}

export async function toMessageContext(
  message: InboundMessage,
  deps: InboundDependencies,
): Promise<WhatsAppMessageContext> {
  const messageId = message.key.id ?? "";
  const chatId = accountJid(message.key.remoteJid);
  if (!messageId || !chatId) {
    throw new Error("incomplete message");
  }
  const server = parseJid(chatId)?.server;
  const chatKind = server === groupServer ? "group" : server === phoneServer || server === anonymousServer ? "direct" : "other";

  const ownUsers = new Set(deps.ownJids().map((jid) => parseJid(jid)?.user).filter((user): user is string => !!user));
  const chatJids = [message.key.remoteJid, message.key.remoteJidAlt];
  const isSelfChat = chatKind === "direct" && chatJids.some((jid) => ownUsers.has(parseJid(jid)?.user ?? ""));

  const authorJids = chatKind === "group" ? [message.key.participant, message.key.participantAlt] : chatJids;
  const senderId = chatKind === "group" ? accountJid(message.key.participant ?? message.key.participantAlt) || chatId : chatId;
  const senderNumbers = await phoneNumbersOf(authorJids, deps.resolveNumber);

  const content = unwrap(message.message);
  const contextInfo = content?.extendedTextMessage?.contextInfo ?? undefined;
  const mentioned = (contextInfo?.mentionedJid ?? []).some((jid) => ownUsers.has(parseJid(jid)?.user ?? ""));
  const repliesToBot = !!contextInfo?.stanzaId && deps.sent.has(contextInfo.stanzaId);

  const now = deps.now ?? Date.now;
  return {
    text: stripOwnMentions(content?.conversation ?? content?.extendedTextMessage?.text ?? "", ownUsers),
    messageId,
    chatId,
    chatKind,
    senderId,
    senderNumbers,
    fromMe: message.key.fromMe === true,
    isSelfChat,
    sentByBot: deps.sent.has(messageId),
    mentionsBot: mentioned || repliesToBot,
    timestampMs: timestampMs(message.messageTimestamp) ?? now(),
    reply: (text) => {
      // The id is remembered before the send: the echo of this message can come back before
      // the send call returns, and it must not be answered as if the owner had typed it.
      const sentId = deps.newMessageId();
      deps.sent.add(sentId);
      return deps.send(chatId, text, { messageId: sentId, ...(chatKind === "group" ? { quoted: message } : {}) });
    },
    typing: (active) => deps.presence(chatId, active),
  };
}

async function phoneNumbersOf(
  jids: Array<string | null | undefined>,
  resolveNumber: InboundDependencies["resolveNumber"],
): Promise<string[]> {
  const numbers: string[] = [];
  for (const jid of jids) {
    const parsed = parseJid(jid);
    if (parsed?.server === phoneServer) {
      numbers.push(parsed.user);
    }
  }
  if (numbers.length > 0 || !resolveNumber) {
    return numbers;
  }

  for (const jid of jids) {
    const parsed = parseJid(jid);
    if (parsed?.server !== anonymousServer) {
      continue;
    }
    try {
      const resolved = parseJid(await resolveNumber(accountJid(jid)));
      if (resolved?.server === phoneServer) {
        numbers.push(resolved.user);
      }
    } catch {
      // An unknown mapping only means the sender cannot be matched to a phone number.
    }
  }
  return numbers;
}

function unwrap(content: InboundContent | null | undefined): InboundContent | undefined {
  let current = content ?? undefined;
  for (let depth = 0; current && depth < 4; depth += 1) {
    const wrapper = wrappers.find((name) => current?.[name]);
    const inner = wrapper ? (current[wrapper] as { message?: InboundContent } | undefined)?.message : undefined;
    if (!inner) {
      break;
    }
    current = inner;
  }
  return current;
}

// A mention is written in the text as "@<user>" (digits only), so it is safe to build a pattern.
function stripOwnMentions(text: string, ownUsers: Set<string>): string {
  let result = text;
  for (const user of ownUsers) {
    if (/^[0-9]+$/.test(user)) {
      result = result.replace(new RegExp(`@${user}\\b`, "g"), "");
    }
  }
  return result.replace(/[ \t]{2,}/g, " ").trim();
}

function timestampMs(value: InboundMessage["messageTimestamp"]): number | undefined {
  const seconds = typeof value === "object" && value !== null ? value.toNumber() : Number(value);
  return Number.isFinite(seconds) && seconds > 0 ? seconds * 1000 : undefined;
}
