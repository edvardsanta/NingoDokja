import { useCallback, useState } from "react";

import type { MemoryStatus } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTranslate } from "../i18n/context.js";
import { WorkspacePanel } from "./workspace_panel.js";
import { CardFrame, OffNotice } from "./card_frame.js";
import {
  parseMemoryStatus,
  scoreViewOf,
  similarityOf,
  type MemoryView,
  type ScoreView,
} from "./memory_model.js";
import { MemoryExperiencesPanel, WaitingVerdictsPanel } from "./memory_experiences.js";
import { KeptFactsPanel } from "./kept_facts.js";
import { useCard, type LoadResult } from "./use_card.js";

// A stopped memory service takes about five seconds to fail, so wait longer than that.
const TIMEOUT_MS = 15_000;

export async function loadMemory(transport: Transport): Promise<LoadResult<MemoryView>> {
  const answer = await transport.request("memory.status", {}, { timeoutMs: TIMEOUT_MS });
  if (!answer.ok) return answer;

  const status = parseMemoryStatus(answer.result);
  if (!status) {
    return { ok: false, error: { code: "unexpected", message: "unexpected memory reply" } };
  }
  if (status.off) return { ok: true, data: { off: true, reason: status.reason } };

  const stats = await transport.request("memory.stats", {}, { timeoutMs: TIMEOUT_MS });
  return { ok: true, data: { off: false, status, score: scoreViewOf(stats) } };
}

export function MemoryCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(() => loadMemory(transport), [transport]);
  const { state, reload, poll } = useCard(load);
  const [verdicts, setVerdicts] = useState(0);
  // A verdict changes the counts and both lists; the owner stays where he is reading.
  const judged = useCallback(() => { setVerdicts((value) => value + 1); poll(); }, [poll]);
  const memory = state.phase === "ready" && !state.data.off ? state.data : undefined;

  // What asks something of the owner comes first, what he keeps next, the record after it and the
  // numbers last. The facts stand outside the memory's frame, so they outlive a stopped service.
  return (
    <WorkspacePanel trail={[t("nav_notebook")]}>
      <CardFrame title={t("memory_title")} state={state} onReload={reload}>
        {(view) => view.off ? <OffNotice reason={view.reason} /> : <>
          <MemoryOpening status={view.status} />
          <WaitingVerdictsPanel transport={transport} onChanged={judged} revision={verdicts} />
        </>}
      </CardFrame>
      <KeptFactsPanel transport={transport} />
      {memory && <>
        <MemoryExperiencesPanel transport={transport} onChanged={judged} revision={verdicts} initial="resolved" />
        <MemoryBody view={memory} />
      </>}
    </WorkspacePanel>
  );
}

// What Ningo tried, said by him once, then plainly: a line in his voice only when something waits for
// the owner or nothing was tried yet; the numbers stay in the plain line.
function MemoryOpening({ status }: { status: MemoryStatus }) {
  const t = useTranslate();
  const voice = status.experiences === 0 ? t("memory_voice_empty") : status.pending > 0 ? t("memory_voice_pending") : "";
  return (
    <div className="memory-opening">
      {voice && <p className="voice">{voice}</p>}
      <p className={voice ? "detail" : undefined}>
        {status.experiences === 0
          ? t("memory_empty_next")
          : t("memory_counts", {
              experiences: status.experiences,
              pending: status.pending,
              resolved: status.resolved,
              expired: status.expired,
            })}
      </p>
    </div>
  );
}

// How the predictions fare, then what an operator needs to know about the memory itself: closed and
// quiet, unless the numbers could not be read.
function MemoryBody({ view }: { view: Extract<MemoryView, { off: false }> }) {
  const t = useTranslate();
  const { status } = view;
  const similarity = similarityOf(status);
  return (
    <details className="wp-section memory-numbers" open={view.score.kind === "error"}>
      <summary>{t("memory_score_title")}</summary>
      <ScoreBody score={view.score} />
      <p className="note" data-tone={similarity === "on" ? "ok" : "unchecked"}>
        {similarity === "on" && t("memory_similarity_on", { model: status.embedModel })}
        {similarity === "off" && t("memory_similarity_off")}
        {similarity === "down" && t("memory_similarity_down")}
      </p>
      {status.needsReindex > 0 && (
        <p className="note" data-tone="unchecked">
          {t("memory_needs_reindex", { count: status.needsReindex })}
        </p>
      )}
    </details>
  );
}

function ScoreBody({ score }: { score: ScoreView }) {
  const t = useTranslate();
  const { fixed, signed } = useNumbers();
  switch (score.kind) {
    case "off":
      return <OffNotice reason={score.reason} />;
    case "error":
      return (
        <p className="note" data-tone="problem">
          {t("memory_score_failed", { message: score.message })}
        </p>
      );
    case "nothing":
      return (
        <p className="note" data-tone="off">
          {t("memory_nothing_scored")}
        </p>
      );
    case "too-few":
    case "judged": {
      const { scored, unscored, minScored, brierPrediction, brierBaseline, skill, beatsBaseline } =
        score.score;
      return (
        <>
          {score.kind === "too-few" && (
            <p className="note" data-tone="unchecked">
              {t("memory_too_few", { scored, needed: minScored })}
            </p>
          )}
          <p>{t("memory_scored_counts", { scored, unscored })}</p>
          <p>
            {t("memory_brier", {
              prediction: fixed(brierPrediction, 3),
              baseline: fixed(brierBaseline, 3),
            })}
          </p>
          {score.kind === "judged" && (
            <p className="note" data-tone={beatsBaseline ? "ok" : "unchecked"}>
              {t(beatsBaseline ? "memory_beats_baseline" : "memory_does_not_beat", {
                skill: signed(skill, 2),
              })}
            </p>
          )}
        </>
      );
    }
  }
}
