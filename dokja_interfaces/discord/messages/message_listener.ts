export type DiscordMessageConfig = {
  botUserId?: string;
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  allowedGuildId?: string;
  allowedChannelIds: string[];
  dmPolicy: string;
};

import { buildBridgePayload, dispatchToOrchestrator } from "../transport/bridge.js";
import type { DiscordMessageContext, DiscordMessageHandler } from "../types.js";

export function createDiscordMessageHandler(config: DiscordMessageConfig): DiscordMessageHandler {
  return async (context: DiscordMessageContext) => {
    if (context.isBot) {
      console.log("[dokja-discord] message ignored", JSON.stringify({ reason: "bot", messageId: context.messageId }));
      return;
    }
    if (!shouldHandleMessage(context, config)) {
      console.log(
        "[dokja-discord] message ignored",
        JSON.stringify({
          reason: "policy",
          messageId: context.messageId,
          channelId: context.channelId,
          guildId: context.guildId,
        }),
      );
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
      const reply = await dispatchToOrchestrator(
        config.orchestratorEndpoint,
        config.orchestratorTimeoutMs,
        buildBridgePayload({
          eventType: "message.created",
          content: context.content,
          userId: context.userId,
          channelId: context.channelId,
          messageId: context.messageId,
          guildId: context.guildId,
        }),
      );
      if (!reply.trim()) {
        console.log(
          "[dokja-discord] message dispatch completed",
          JSON.stringify({ messageId: context.messageId, replyLength: 0 }),
        );
        return;
      }

      await context.reply(reply);
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

function shouldHandleMessage(context: DiscordMessageContext, config: DiscordMessageConfig): boolean {
  if (context.isDirectMessage) {
    return config.dmPolicy === "open";
  }
  if (config.allowedGuildId && context.guildId !== config.allowedGuildId) {
    return false;
  }
  if (config.allowedChannelIds.length > 0 && !config.allowedChannelIds.includes(context.channelId)) {
    return false;
  }
  if (!config.botUserId) {
    return true;
  }
  return context.mentionsBot;
}
