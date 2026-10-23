import { useCallback, useEffect, useState } from "react";
import { isRecord } from "../../shared/records.js";
import type { MemoryExperience, MemoryExperienceDetail, MemoryExperiences, Off } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTimes, useTranslate } from "../i18n/context.js";
import { OffNotice, Pending, Problem } from "./card_frame.js";
import { ExperienceFeedback } from "./experience_feedback.js";
import { useAction } from "./use_action.js";
import type { MessageId } from "../i18n/i18n.js";
import { useCard, type LoadResult } from "./use_card.js";
import { useWorkspaceRefresh } from "./workspace_panel.js";

type Filter = "all" | "pending" | "resolved";

const FILTERS: readonly { id: Filter; labelId: MessageId }[] = [
  { id: "all", labelId: "experiences_all" },
  { id: "pending", labelId: "experiences_filter_waiting" },
  { id: "resolved", labelId: "experiences_filter_settled" },
];

export function MemoryExperiencesPanel({ transport, onChanged }: { transport: Transport; onChanged?: () => void }) {
  const t = useTranslate();
  const [revision, setRevision] = useState(0);
  const changed = () => { setRevision((value) => value + 1); onChanged?.(); };
  const [filter, setFilter] = useState<Filter>("all");
  const [offset, setOffset] = useState(0);
  const load = useCallback(async (at: number): Promise<LoadResult<MemoryExperiences | Off>> => {
    const answer = await transport.request("memory.list", { state: filter, limit: 20, offset: at }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || (answer.result.off !== true && !Array.isArray(answer.result.experiences))) return unexpected();
    return { ok: true, data: answer.result as MemoryExperiences | Off };
  }, [transport, filter]);
  const listing = useAction(load);
  // What was tried is what the Notebook is for, so it is read when the view opens, again when the
  // filter or the page changes, and with the panel's refresh.
  useEffect(() => { listing.start(offset); }, [offset, revision, listing.start]);
  const reread = useCallback(() => listing.start(offset), [listing.start, offset]);
  useWorkspaceRefresh(reread, listing.state.phase === "loading");
  const page = listing.state.phase === "ready" ? listing.state.data : undefined;
  return <section aria-label={t("experiences_title")}>
    <h3>{t("experiences_title")}</h3>
    {/* a filter is a mode of the list, so it is a set of pressed buttons, never a switch */}
    <div className="seg" role="group" aria-label={t("experiences_filter")}>
      {FILTERS.map(({ id, labelId }) => (
        <button key={id} type="button" aria-pressed={filter === id} onClick={() => { setFilter(id); setOffset(0); }}>{t(labelId)}</button>
      ))}
    </div>
    {listing.state.phase === "loading" && <Pending />}
    {listing.state.phase === "error" && <Problem error={listing.state.error} />}
    {page?.off && <OffNotice reason={page.reason} />}
    {page && !page.off && <>
      <p>{t("experiences_total", { count: page.total })}</p>
      {!page.experiences.length && <p>{t("experiences_empty")}</p>}
      <ul className="hits">{page.experiences.map((entry) => <ExperienceRow key={`${filter}-${offset}-${entry.ref}`} entry={entry} transport={transport} onChanged={changed} />)}</ul>
      <div className="pager">
        <button type="button" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 20))}>{t("experiences_previous")}</button>
        <button type="button" disabled={!page.experiences.length || offset + page.experiences.length >= page.total} onClick={() => setOffset(offset + 20)}>{t("experiences_next")}</button>
      </div>
    </>}
  </section>;
}

// What Ningo decided, said as a decision, and what came of it: experience is how he acted on what
// he knew. The record's id and exact time stay for the operator, in the tooltip.
const VERDICTS: Record<string, { id: MessageId; tone: string }> = {
  accepted: { id: "experience_verdict_accepted", tone: "ok" },
  kept: { id: "experience_verdict_kept", tone: "ok" },
  ignored: { id: "experience_verdict_ignored", tone: "off" },
  replaced: { id: "experience_verdict_replaced", tone: "unchecked" },
  expired: { id: "experience_verdict_expired", tone: "off" },
};

function decisionOf(entry: MemoryExperience, t: ReturnType<typeof useTranslate>): string {
  if (entry.action === "hashtag.suggest" && entry.detail) return t("experience_decision_hashtag", { tag: entry.detail });
  return entry.detail ? `${entry.action}: ${entry.detail}` : entry.action;
}

function ExperienceRow({ entry, transport, onChanged }: { entry: MemoryExperience; transport: Transport; onChanged: () => void }) {
  const t = useTranslate();
  const { ago } = useTimes();
  const [opened, setOpened] = useState(false);
  const verdict = entry.outcome ? VERDICTS[entry.outcome] : undefined;
  return <li className="decision">
    <p className="decision-line">
      <strong>{decisionOf(entry, t)}</strong>
      <span className="decision-verdict" data-tone={verdict?.tone ?? (entry.outcome ? "other" : "unchecked")}>
        {verdict ? t(verdict.id) : entry.outcome || t("experience_verdict_pending")}
      </span>
    </p>
    <p className="detail" title={`${entry.ref} · ${entry.createdAt}`}>{entry.createdAt ? ago(entry.createdAt) : entry.ref}</p>
    {entry.contextSnippet && <p className="decision-context">{t("experience_about", { context: entry.contextSnippet })}</p>}
    <button type="button" className="quiet" aria-expanded={opened} aria-label={t("experience_inspect_named", { ref: entry.ref })} onClick={() => setOpened((value) => !value)}>{t(opened ? "experience_close" : "experience_inspect")}</button>
    {opened && <ExperienceDetail transport={transport} entry={entry} onChanged={onChanged} />}
  </li>;
}

function ExperienceDetail({ transport, entry, onChanged }: { transport: Transport; entry: MemoryExperience; onChanged: () => void }) {
  const load = useCallback(async (): Promise<LoadResult<MemoryExperienceDetail | Off>> => {
    const answer = await transport.request("memory.get", { ref: entry.ref }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    const raw = answer.result;
    if (!isRecord(raw)) return unexpected();
    if (raw.off === true || raw.found === false) return { ok: true, data: raw as MemoryExperienceDetail | Off };
    if (raw.found !== true || !isRecord(raw.experience) || raw.experience.ref !== entry.ref) return unexpected();
    return { ok: true, data: raw as MemoryExperienceDetail };
  }, [transport, entry.ref]);
  const { state, reload } = useCard(load);
  const t = useTranslate();
  return <section className="experience-detail" aria-label={t("experience_details")}>
    {state.phase === "loading" && <Pending />}
    {state.phase === "error" && <><Problem error={state.error} /><button type="button" onClick={reload}>{t("retry")}</button></>}
    {state.phase === "ready" && (state.data.off ? <OffNotice reason={state.data.reason} /> : !state.data.found ? <p>{t("experience_missing")}</p> : <><ExperienceBody entry={state.data.experience} fallback={entry.contextSnippet} /><ExperienceFeedback entry={state.data.experience} transport={transport} onChanged={onChanged} /></>)}
  </section>;
}

function ExperienceBody({ entry, fallback }: { entry: MemoryExperience; fallback: string }) {
  const t = useTranslate();
  const { fixed } = useNumbers();
  const p = (value: number | null) => value === null ? t("experience_not_recorded") : `${fixed(value * 100, 1)}%`;
  const feedback = () => {
    switch (entry.feedback.code) {
      case "hashtag_tag": return t("experience_feedback_hashtag");
      case "explicit_outcome": return t("experience_feedback_explicit");
      case "expired": return t("experience_feedback_expired");
      case "recorded": return t("experience_feedback_recorded");
      default: return entry.feedback.message || t("experience_feedback_unknown");
    }
  };
  return <>
    <dl>
      <dt>{t("experience_action")}</dt><dd>{entry.action}</dd>
      <dt>{t("experience_action_detail")}</dt><dd>{entry.detail || t("experience_no_detail")}</dd>
      <dt>{t("experience_prediction")}</dt><dd>{p(entry.predictedP)}</dd>
      <dt>{t("experience_baseline")}</dt><dd>{p(entry.baselineP)}</dd>
      <dt>{t("experience_similarity")}</dt><dd>{p(entry.matchedScore)}</dd>
      <dt>{t("experience_outcome")}</dt><dd>{entry.outcome || t("experience_no_outcome")}</dd>
      {entry.resolvedAt && <><dt>{t("experience_resolved_at")}</dt><dd>{entry.resolvedAt}</dd></>}
    </dl>
    <h4>{t(entry.context === undefined ? "experience_context_excerpt" : "experience_context")}</h4>
    <pre className="document-text">{entry.context ?? fallback ?? t("experience_not_recorded")}</pre>
    {entry.contextTruncated && <p>{t("experience_context_truncated")}</p>}
    {entry.matchedSnippet && <><h4>{t("experience_matched_excerpt")}</h4><p>{entry.matchedSnippet}</p></>}
    <h4>{t("experience_feedback")}</h4><p>{feedback()}</p>
    <p className="detail">{t("experience_feedback_scope")}</p>
  </>;
}

function unexpected(): LoadResult<never> { return { ok: false, error: { code: "unexpected", message: "unexpected experience reply" } }; }
