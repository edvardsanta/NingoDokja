import { useState } from "react";
import type { Transport, TransportError } from "../../shared/transport.js";
import type { AnswerFeedback, AnswerVerdict } from "../../shared/replies.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";
import { Button } from "./ui.js";

type State =
  | { kind: "open" }
  | { kind: "sending"; verdict: AnswerVerdict }
  | { kind: "kept"; verdict: AnswerVerdict; earlier: boolean }
  | { kind: "unrecorded" }
  | { kind: "failed"; error: TransportError };

// The owner's verdict on one answer: useful or wrong. It is what Ningo learns from (step 08.3), so
// it is a deliberate press, kept once; the first verdict stands. Not judging is fine: an answer
// nobody judges simply ages out and never counts as wrong.
export function AnswerVerdictBar({ transport, conversationId, turnId, seq }: { transport: Transport; conversationId: string; turnId: string; seq: number }) {
  const t = useTranslate();
  const [state, setState] = useState<State>({ kind: "open" });

  async function judge(verdict: AnswerVerdict) {
    setState({ kind: "sending", verdict });
    const answer = await transport.request("conversation.feedback", { conversation_id: conversationId, turn_id: turnId, seq, verdict }).catch(() => undefined);
    if (!answer) {
      setState({ kind: "failed", error: { code: "unavailable", message: "" } });
      return;
    }
    if (!answer.ok) {
      setState({ kind: "failed", error: answer.error });
      return;
    }
    const result = isRecord(answer.result) ? (answer.result as AnswerFeedback) : undefined;
    if (!result || !result.found) {
      // An answer from before verdicts were kept has nothing to resolve; say so once, quietly.
      setState({ kind: "unrecorded" });
      return;
    }
    const kept = result.verdict ?? verdict;
    setState({ kind: "kept", verdict: kept, earlier: !result.resolved });
  }

  if (state.kind === "kept") {
    const id = state.verdict === "useful" ? (state.earlier ? "answer_verdict_earlier_useful" : "answer_verdict_kept_useful") : (state.earlier ? "answer_verdict_earlier_wrong" : "answer_verdict_kept_wrong");
    return <small className="answer-verdict" role="status">{t(id)}</small>;
  }
  if (state.kind === "unrecorded") return <small className="answer-verdict" role="status">{t("answer_verdict_unrecorded")}</small>;
  const sending = state.kind === "sending";
  return <div className="answer-verdict" role="group" aria-label={t("answer_verdict_label")}>
    <Button size="compact" disabled={sending} aria-pressed={sending && state.verdict === "useful"} onClick={() => void judge("useful")}>{t("answer_verdict_useful")}</Button>
    <Button size="compact" disabled={sending} aria-pressed={sending && state.verdict === "wrong"} onClick={() => void judge("wrong")}>{t("answer_verdict_wrong")}</Button>
    {state.kind === "failed" && <small role="alert">{t(state.error.code === "denied" ? "answer_verdict_denied" : "answer_verdict_failed")}</small>}
  </div>;
}
