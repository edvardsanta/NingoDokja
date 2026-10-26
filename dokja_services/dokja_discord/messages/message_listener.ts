export type DiscordMessageConfig = {
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  locale?: string;
};

import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import type { DiscordMessageContext, DiscordMessageHandler } from "../types.js";
import { localizeNotice } from "./notices.js";

export function createDiscordMessageHandler(config: DiscordMessageConfig): DiscordMessageHandler {
  return async (context: DiscordMessageContext) => {
    if (context.isBot) {
      console.log("[dokja-discord] message ignored", JSON.stringify({ reason: "bot", messageId: context.messageId }));
      return;
    }
    if (!context.content) {
      console.log("[dokja-discord] message ignored", JSON.stringify({ reason: "empty", messageId: context.messageId }));
      return;
    }

    console.log(
      "[dokja-discord] message dispatch",
      JSON.stringify({
        channelId: context.channelId,
        guildId: context.guildId,
        userId: context.userId,
        messageId: context.messageId,
        contentLength: context.content.length,
      }),
    );

    try {
      const response = await dispatchToOrchestratorResponse(
        config.orchestratorEndpoint,
        config.orchestratorTimeoutMs,
        buildBridgePayload({
          eventType: "message.created",
          content: context.content,
          replyToContent: context.replyToContent,
          mentionedUserIds: context.mentionedUserIds,
          userId: context.userId,
          channelId: context.channelId,
          messageId: context.messageId,
          guildId: context.guildId,
          entryKind: "message",
          isDirectMessage: context.isDirectMessage,
          mentionsBot: context.mentionsBot,
        }),
      );
      const reply = localizeNotice(response.notice, response.reply, config.locale);
      if (!reply.trim()) {
        console.log(
          "[dokja-discord] message dispatch completed",
          JSON.stringify({ messageId: context.messageId, replyLength: 0 }),
        );
        return;
      }

      await context.reply(response.attachments?.length
        ? { content: reply, attachments: response.attachments }
        : reply);
      console.log(
        "[dokja-discord] message reply sent",
        JSON.stringify({ messageId: context.messageId, replyLength: reply.length }),
      );
    } catch (error) {
      console.error(
        "[dokja-discord] message dispatch failed",
        JSON.stringify({
          messageId: context.messageId,
          error: error instanceof Error ? error.message : String(error),
        }),
      );
      throw error;
    }
  };
}
