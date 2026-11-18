export type DiscordMessageConfig = {
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  locale?: string;
  // Backoff between late retries of a failed dispatch; an exhausted schedule
  // gives up with an honest notice. Production waits minutes; tests pass a
  // short list to run the retries within the suite.
  retryDelaysMs?: number[];
};

import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import type { DiscordMessageContext, DiscordMessageHandler } from "../types.js";
import { localizeNotice, dispatchFailureNotice, dispatchRetriesExhaustedNotice } from "./notices.js";
import { formatCitations } from "./citations.js";

// The default late-retry schedule: soon, later, much later. A dispatch usually
// fails because the orchestrator is over its deadline or unreachable, conditions
// that pass; a resend minutes later answers where the sender already waits.
const defaultRetryDelaysMs = [60_000, 300_000, 900_000];

type DeferredDispatch = {
  body: ReturnType<typeof buildBridgePayload>;
  // Retries already spent; the next one indexes the schedule.
  attempt: number;
  timer: ReturnType<typeof setTimeout>;
};

export function createDiscordMessageHandler(config: DiscordMessageConfig): DiscordMessageHandler {
  // Failed dispatches are retried in the background and answered late, replying
  // to the original message, instead of leaving the sender with silence. The
  // queue is in-memory: a restart loses it, and the interface then asked the
  // sender to try again.
  const retryDelays = config.retryDelaysMs ?? defaultRetryDelaysMs;
  const deferred = new Map<string, DeferredDispatch>();

  const deliver = async (context: DiscordMessageContext, body: ReturnType<typeof buildBridgePayload>) => {
    const response = await dispatchToOrchestratorResponse(config.orchestratorEndpoint, config.orchestratorTimeoutMs, body);
    const reply = localizeNotice(response.notice, formatCitations(response.reply), config.locale);
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
  };

  const scheduleRetry = (context: DiscordMessageContext, body: ReturnType<typeof buildBridgePayload>, attempt: number) => {
    const delay = retryDelays[attempt];
    if (delay === undefined) {
      // The schedule ran out: close the promise instead of letting it dangle.
      context.reply(dispatchRetriesExhaustedNotice(config.locale)).catch(() => {});
      return;
    }
    const timer = setTimeout(() => {
      deferred.delete(context.messageId);
      deliver(context, body).catch(() => scheduleRetry(context, body, attempt + 1));
    }, delay);
    timer.unref?.();
    const pending = deferred.get(context.messageId);
    if (pending) clearTimeout(pending.timer);
    deferred.set(context.messageId, { body, attempt, timer });
  };

  return async (context: DiscordMessageContext) => {
    if (context.isBot) {
      console.log("[dokja-discord] message ignored", JSON.stringify({ reason: "bot", messageId: context.messageId }));
      return;
    }
    if (!context.content && !context.documentAttachments?.length && !context.mediaAttachments?.length) {
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

    const body = buildBridgePayload({
      eventType: "message.created",
      content: context.content,
      documentAttachments: context.documentAttachments,
      mediaAttachments: context.mediaAttachments,
      replyTo: context.replyTo,
      messageEmbeds: context.messageEmbeds,
      locale: config.locale,
      replyToContent: context.replyToContent,
      mentionedUserIds: context.mentionedUserIds,
      mentionedUsers: context.mentionedUsers,
      userId: context.userId,
      userName: context.userName,
      channelId: context.channelId,
      messageId: context.messageId,
      guildId: context.guildId,
      entryKind: "message",
      isDirectMessage: context.isDirectMessage,
      mentionsBot: context.mentionsBot,
    });

    try {
      await deliver(context, body);
      const pending = deferred.get(context.messageId);
      if (pending) {
        // A fresh dispatch that answers the message cancels its late retry, so a
        // recovered message update never doubles an answer the retry would post.
        clearTimeout(pending.timer);
        deferred.delete(context.messageId);
      }
    } catch (error) {
      console.error(
        "[dokja-discord] message dispatch failed",
        JSON.stringify({
          messageId: context.messageId,
          error: error instanceof Error ? error.message : String(error),
        }),
      );
      const pending = deferred.get(context.messageId);
      scheduleRetry(context, body, pending?.attempt ?? 0);
      if (!pending) {
        // First failure for this message: promise the late retry. A failed retry
        // only reschedules; the promise was already made.
        try {
          await context.reply(dispatchFailureNotice(config.locale));
        } catch (replyError) {
          console.error(
            "[dokja-discord] dispatch failure notice failed",
            JSON.stringify({
              messageId: context.messageId,
              error: replyError instanceof Error ? replyError.message : String(replyError),
            }),
          );
        }
      }
      throw error;
    }
  };
}
