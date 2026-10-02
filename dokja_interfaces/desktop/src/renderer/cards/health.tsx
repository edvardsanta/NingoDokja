import { useCallback, useEffect, type CSSProperties } from "react";

import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { usePulse } from "../pulse.js";
import { CardFrame } from "./card_frame.js";
import { parseHealth, pulseOf, type Health } from "./health_model.js";
import { useCard, type LoadResult } from "./use_card.js";

// Longer than the few seconds the orchestrator takes to give up on a stopped service.
const TIMEOUT_MS = 15_000;

const STATUS_IDS: Record<string, MessageId> = {
  ok: "status_ok",
  error: "status_error",
  stopped: "status_stopped",
  degraded: "status_degraded",
  disabled: "status_disabled",
  unchecked: "status_unchecked",
};

export async function loadHealth(transport: Transport): Promise<LoadResult<Health>> {
  const result = await transport.request("ningo.status", {}, { timeoutMs: TIMEOUT_MS });
  if (!result.ok) return result;
  const health = parseHealth(result.result);
  return health
    ? { ok: true, data: health }
    : { ok: false, error: { code: "unexpected", message: "unexpected status reply" } };
}

export function HealthCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const { report } = usePulse();
  const load = useCallback(() => loadHealth(transport), [transport]);
  const { state, reload } = useCard(load);

  useEffect(() => {
    report(pulseOf(state));
  }, [state, report]);

  return (
    <CardFrame kindLabel={t("kind_health")} title={t("health_title")} state={state} onReload={reload}>
      {(health) => <HealthBody health={health} />}
    </CardFrame>
  );
}

function HealthBody({ health }: { health: Health }) {
  const t = useTranslate();
  if (health.rows.length === 0) return <p>{t("health_empty")}</p>;

  const { ok, problem, off, unchecked } = health.counts;
  const summary = t("health_summary", { ok, problem, off });
  return (
    <>
      <div className="strip" aria-hidden="true">
        {health.rows.map((row) => (
          <span key={row.name} data-tone={row.tone} />
        ))}
      </div>
      <p className="summary">
        {unchecked > 0 ? `${summary}, ${t("health_unchecked", { count: unchecked })}` : summary}
      </p>
      <ul className="services">
        {health.rows.map((row, index) => {
          const label = STATUS_IDS[row.status];
          return (
            <li key={row.name} data-tone={row.tone} style={{ "--i": index } as CSSProperties}>
              <span className="service-name">{row.name}</span>
              <span className="service-status">{label ? t(label) : row.status}</span>
              {row.detail && <span className="service-detail">{row.detail}</span>}
            </li>
          );
        })}
      </ul>
    </>
  );
}
