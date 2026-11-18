export type DiscordMessageConfig = {
  orchestratorEndpoint: string;
  orchestratorTimeoutMs: number;
  locale?: string;
  // Backoff between late retries of a failed dispatch; an exhausted schedule
  // gives up with an honest notice. Production waits minutes; tests pass a
  // short list to run the retries within the suite.
  retryDelaysMs?: number[];
  // Persists deferred dispatches across restarts; without it the retries live
  // only in this process and a restart drops the queue.
  deferralStore?: DeferredDispatchStore;
  // Answers a late retry as a reply to the original message. With a store this
  // is required: after a restart the live message context no longer exists, and
  // the late answer delivers through the delivery path instead.
  lateReply?: (target: { channelId: string; messageId: string }, reply: DiscordReply) => Promise<void>;
};

import { buildBridgePayload, dispatchToOrchestratorResponse } from "../transport/bridge.js";
import type { DiscordMessageContext, DiscordMessageHandler, DiscordReply } from "../types.js";
import { localizeNotice, dispatchFailureNotice, dispatchRetriesExhaustedNotice } from "./notices.js";
import { formatCitations } from "./citations.js";
import type { DeferredDispatchJob, DeferredDispatchStore } from "./deferred_dispatch.js";

// The default late-retry schedule: soon, later, much later. A dispatch usually
// fails because the orchestrator is over its deadline or unreachable, conditions
// that pass; a resend minutes later answers where the sender already waits.
const defaultRetryDelaysMs = [60_000, 300_000, 900_000];

export type DiscordMessageHandling = {
  handler: DiscordMessageHandler;
  // Re-arms every deferred dispatch the store holds, e.g. on boot after a restart.
  restoreDeferrals(): void;
};

export function createDiscordMessageHandler(config: DiscordMessageConfig): DiscordMessageHandling {
  const retryDelays = config.retryDelaysMs ?? defaultRetryDelaysMs;
  // Timers are only triggers for the stored next_retry_at; the store, when
  // configured, is the source of truth and survives a restart.
  const timers = new Map<string, ReturnType<typeof setTimeout>>();

  const compose = (response: Awaited<ReturnType<typeof dispatchToOrchestratorResponse>>) => ({
    text: localizeNotice(response.notice, formatCitations(response.reply), config.locale),
    attachments: response.attachments ?? [],
  });

  const clearTimer = (messageId: string) => {
    const timer = timers.get(messageId);
    if (timer) clearTimeout(timer);
    timers.delete(messageId);
  };

  const giveUp = async (job: DeferredDispatchJob) => {
    config.deferralStore?.delete(job.messageId);
    try {
      await config.lateReply?.({ channelId: job.channelId, messageId: job.messageId }, dispatchRetriesExhaustedNotice(config.locale));
    } catch (error) {
      console.error(
        "[dokja-discord] deferred give-up notice failed",
        JSON.stringify({ messageId: job.messageId, error: error instanceof Error ? error.message : String(error) }),
      );
    }
  };

  // Schedules retry number `attempt` (1-based) for a deferred dispatch. An
  // attempt past the schedule closes the promise instead of rescheduling.
  const scheduleRetry = (job: Omit<DeferredDispatchJob, "attempt" | "nextRetryAt">, attempt: number) => {
    const delay = retryDelays[attempt - 1];
    if (delay === undefined) {
      void giveUp({ ...job, attempt, nextRetryAt: Date.now() });
      return;
    }
    const next: DeferredDispatchJob = { ...job, attempt, nextRetryAt: Date.now() + delay };
    config.deferralStore?.upsert(next);
    const timer = setTimeout(() => {
      timers.delete(next.messageId);
      void runDeferred(next);
    }, Math.max(0, next.nextRetryAt - Date.now()));
    timer.unref?.();
    clearTimer(next.messageId);
    timers.set(next.messageId, timer);
  };

  const runDeferred = async (job: DeferredDispatchJob) => {
    try {
      const response = await dispatchToOrchestratorResponse(config.orchestratorEndpoint, config.orchestratorTimeoutMs, job.body);
      const composed = compose(response);
      if (composed.text.trim()) {
        await config.lateReply?.(
          { channelId: job.channelId, messageId: job.messageId },
          composed.attachments.length ? { content: composed.text, attachments: composed.attachments } : composed.text,
        );
      }
      config.deferralStore?.delete(job.messageId);
    } catch (error) {
      console.error(
        "[dokja-discord] deferred dispatch retry failed",
        JSON.stringify({ messageId: job.messageId, error: error instanceof Error ? error.message : String(error) }),
      );
      scheduleRetry(job, job.attempt + 1);
    }
  };

  const restoreDeferrals = () => {
    const jobs = config.deferralStore?.pending() ?? [];
    for (const job of jobs) {
      armRestored(job);
    }
    if (jobs.length > 0) {
      console.log("[dokja-discord] deferred dispatches restored", JSON.stringify({ count: jobs.length }));
    }
  };

  const armRestored = (job: DeferredDispatchJob) => {
    const timer = setTimeout(() => {
      timers.delete(job.messageId);
      void runDeferred(job);
    }, Math.max(0, job.nextRetryAt - Date.now()));
    timer.unref?.();
    timers.set(job.messageId, timer);
  };

  const deliver = async (context: DiscordMessageContext, body: ReturnType<typeof buildBridgePayload>) => {
    const response = await dispatchToOrchestratorResponse(config.orchestratorEndpoint, config.orchestratorTimeoutMs, body);
    const composed = compose(response);
    if (!composed.text.trim()) {
      console.log(
        "[dokja-discord] message dispatch completed",
        JSON.stringify({ messageId: context.messageId, replyLength: 0 }),
      );
      return;
    }
    await context.reply(composed.attachments.length
      ? { content: composed.text, attachments: composed.attachments }
      : composed.text);
    console.log(
      "[dokja-discord] message reply sent",
      JSON.stringify({ messageId: context.messageId, replyLength: composed.text.length }),
    );
  };

  const handler: DiscordMessageHandler = async (context: DiscordMessageContext) => {
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
      if (timers.has(context.messageId)) {
        // A fresh dispatch that answers the message cancels its late retry, so a
        // recovered message update never doubles an answer the retry would post.
        clearTimer(context.messageId);
        config.deferralStore?.delete(context.messageId);
      }
    } catch (error) {
      console.error(
        "[dokja-discord] message dispatch failed",
        JSON.stringify({
          messageId: context.messageId,
          error: error instanceof Error ? error.message : String(error),
        }),
      );
      const firstFailure = !timers.has(context.messageId);
      scheduleRetry(
        { messageId: context.messageId, channelId: context.channelId, userId: context.userId, body },
        1,
      );
      if (firstFailure) {
        // First failure for this message: promise the late retry. Failed retries
        // only reschedule; the promise was already made.
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

  return { handler, restoreDeferrals };
}
