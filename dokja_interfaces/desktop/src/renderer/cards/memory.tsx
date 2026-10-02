import { useCallback } from "react";

import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice } from "./card_frame.js";
import {
  parseMemoryStatus,
  scoreViewOf,
  similarityOf,
  type MemoryView,
  type ScoreView,
} from "./memory_model.js";
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
  const { state, reload } = useCard(load);

  return (
    <CardFrame kindLabel={t("kind_memory")} title={t("memory_title")} state={state} onReload={reload}>
      {(view) => <MemoryBody view={view} />}
    </CardFrame>
  );
}

const fixed = (value: number, digits: number) => value.toFixed(digits);
const signed = (value: number) => `${value >= 0 ? "+" : ""}${value.toFixed(2)}`;

function MemoryBody({ view }: { view: MemoryView }) {
  const t = useTranslate();
  if (view.off) return <OffNotice reason={view.reason} />;

  const { status } = view;
  const similarity = similarityOf(status);
  return (
    <>
      <p>
        {t("memory_counts", {
          experiences: status.experiences,
          pending: status.pending,
          resolved: status.resolved,
          expired: status.expired,
        })}
      </p>
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
      <h3>{t("memory_score_title")}</h3>
      <ScoreBody score={view.score} />
    </>
  );
}

function ScoreBody({ score }: { score: ScoreView }) {
  const t = useTranslate();
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
                skill: signed(skill),
              })}
            </p>
          )}
        </>
      );
    }
  }
}
