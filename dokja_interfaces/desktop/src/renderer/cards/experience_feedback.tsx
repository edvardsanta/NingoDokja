import { useCallback, useState } from "react";
import { isRecord } from "../../shared/records.js";
import type { MemoryExperience, MemoryResolution, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { OffNotice, Pending, Problem } from "./card_frame.js";
import { useAction } from "./use_action.js";
import type { LoadResult } from "./use_card.js";

export function ExperienceFeedback({ entry, transport, onChanged }: {
  entry: MemoryExperience; transport: Transport; onChanged: () => void;
}) {
  const t = useTranslate();
  const [observed, setObserved] = useState("");
  const submit = useCallback(async (choice: Record<string, string>): Promise<LoadResult<MemoryResolution | Off>> => {
    const answer = await transport.request("memory.resolve", { ref: entry.ref, ...choice }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    const result = answer.result;
    if (!isRecord(result) || (result.off !== true && (typeof result.found !== "boolean" || typeof result.resolved !== "boolean"))) {
      return { ok: false, error: { code: "unexpected", message: "unexpected feedback reply" } };
    }
    return { ok: true, data: result as MemoryResolution | Off };
  }, [transport, entry.ref]);
  const { state, start } = useAction(submit);
  // Expiry is not a verdict: a late, explicit observation can still resolve it.
  if (entry.resolvedAt || (entry.outcome && entry.outcome !== "expired")) return null;
  const pending = state.phase === "loading";
  const result = state.phase === "ready" ? state.data : undefined;
  const settled = result && !result.off ? result : undefined;
  return <div>
    {settled ? <>
      <p role="status">{t(!settled.found ? "experience_missing" : settled.resolved ? "experience_feedback_saved" : "experience_feedback_conflict", { outcome: settled.outcome })}</p>
      <button type="button" onClick={onChanged}>{t("experience_feedback_refresh")}</button>
    </> : <fieldset disabled={pending}>
      <legend>{t("experience_feedback_record")}</legend>
      <p>{t("experience_feedback_confirm_scope")}</p>
      <button type="button" onClick={() => start({ outcome: "accepted" })}>{t("experience_feedback_accept")}</button>{" "}
      <button type="button" onClick={() => start({ outcome: "ignored" })}>{t("experience_feedback_ignore")}</button>
      {entry.detail && <form onSubmit={(event) => { event.preventDefault(); if (observed.trim()) start({ observed: observed.trim() }); }}>
        <label>{t("experience_feedback_observed")} <input value={observed} maxLength={200} onChange={(event) => setObserved(event.target.value)} /></label>{" "}
        <button type="submit" disabled={!observed.trim()}>{t("experience_feedback_correct")}</button>
      </form>}
    </fieldset>}
    {pending && <Pending />}
    {state.phase === "error" && <><Problem error={state.error} /><p>{t("experience_feedback_check")}</p><button type="button" onClick={onChanged}>{t("experience_feedback_refresh")}</button></>}
    {result?.off && <OffNotice reason={result.reason} />}
  </div>;
}
