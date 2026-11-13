import type { ReplyContext } from "../../shared/replies.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";

// Why the macro context was not asked, by the orchestrator's own code. Any other reason is said
// plainly as "not needed".
const SKIPPED: Record<string, MessageId> = {
  short_follow_up: "reply_context_skipped_short_follow_up",
  live_context_covers_it: "reply_context_skipped_live_context_covers_it",
  empty_question: "reply_context_skipped_empty_question",
};

// What became of the memory when it was asked, or needed and not available.
const ASKED = {
  recalled: "reply_context_recalled",
  nothing_found: "reply_context_nothing_found",
  unavailable: "reply_context_unavailable",
} as const satisfies Record<Exclude<ReplyContext["macro"], "skipped">, MessageId>;

// What an answer read besides the question, in one quiet line under it: how much of the conversation
// itself (the live context) and what became of the macro context, the memory of earlier discussions.
export function ReplyContextLine({ context }: { context: ReplyContext }) {
  const t = useTranslate();
  const macro = context.macro === "skipped"
    ? t(SKIPPED[context.reason] ?? "reply_context_skipped")
    : t(ASKED[context.macro]);
  return <small className="assistant-by reply-context">{t("reply_context", { live: context.liveMessages, macro })}</small>;
}
