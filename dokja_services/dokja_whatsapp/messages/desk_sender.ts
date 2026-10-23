import type { Conversation } from "../config.js";
import type { SendLimit } from "../runtime/send_limit.js";

export type DeliverOutcome = "sent" | "not_named" | "not_connected" | "limited" | "failed";

type Options = {
  conversations: Conversation[];
  // Sends text into a chat as the linked account and gives the id it went out with.
  send: (jid: string, text: string) => Promise<string | undefined>;
  connected: () => boolean;
  limit: SendLimit;
  // Called once the message went out, so it is part of the recorded conversation.
  record?: (alias: string, text: string, id: string | undefined) => Promise<void>;
};

// What the owner types in the desktop goes out through the linked account, and only into a
// conversation they named. The chat is found by the alias; the number or group id never comes from
// the request. The order matters: a message that is refused costs nothing from the budget.
export function createDeskSender(options: Options) {
  return async (alias: string, text: string): Promise<DeliverOutcome> => {
    const conversation = options.conversations.find((entry) => entry.alias === alias);
    if (!conversation) {
      return "not_named";
    }
    if (!options.connected()) {
      return "not_connected";
    }
    if (!options.limit.take()) {
      return "limited";
    }
    const jid = conversation.group ?? `${conversation.number}@s.whatsapp.net`;
    let id: string | undefined;
    try {
      id = await options.send(jid, text);
    } catch {
      return "failed";
    }
    // A history that cannot be written never turns a sent message into a failure.
    await options.record?.(alias, text, id).catch(() => undefined);
    return "sent";
  };
}
