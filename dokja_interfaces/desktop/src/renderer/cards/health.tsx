import { useCallback, useEffect, useState, type CSSProperties, type FormEvent } from "react";

import type { JobState } from "../../shared/replies.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { usePulse } from "../pulse.js";
import { CardFrame, Problem } from "./card_frame.js";
import {
  formatInterval,
  intervalFields,
  intervalOf,
  parseHealth,
  pulseOf,
  type Health,
  type IntervalUnit,
} from "./health_model.js";
import { useAction } from "./use_action.js";
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

// What the screen can ask the orchestrator to switch. A switch carries the flag and nothing else; an
// interval carries the interval and nothing else, so changing one never touches the other.
type Change =
  | { type: "services.set"; payload: { name: string; enabled: boolean } }
  | { type: "scheduler.jobs.set"; payload: { name: string; enabled?: boolean; interval?: string } };

async function applyChange(transport: Transport, change: Change): Promise<LoadResult<true>> {
  const answer = await transport.request(change.type, change.payload, { timeoutMs: TIMEOUT_MS });
  return answer.ok ? { ok: true, data: true } : answer;
}

export function HealthCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const { report } = usePulse();
  const load = useCallback(() => loadHealth(transport), [transport]);
  const { state, reload } = useCard(load);
  const run = useCallback((change: Change) => applyChange(transport, change), [transport]);
  const change = useAction(run);
  const [failure, setFailure] = useState<TransportError>();

  useEffect(() => {
    report(pulseOf(state));
  }, [state, report]);

  // The orchestrator's own state is read again after every change, whether or not it went through
  // (a timeout does not say what happened), so the screen never shows what it only hoped for. The
  // failure lives here and not in the body, which the reload replaces.
  useEffect(() => {
    if (change.state.phase === "ready") {
      setFailure(undefined);
      reload();
    } else if (change.state.phase === "error") {
      setFailure(change.state.error);
      reload();
    }
  }, [change.state, reload]);

  const refresh = useCallback(() => {
    setFailure(undefined);
    reload();
  }, [reload]);

  return (
    <CardFrame kindLabel={t("kind_health")} title={t("health_title")} state={state} onReload={refresh}>
      {(health) => (
        <HealthBody health={health} busy={change.state.phase === "loading"} failure={failure} onChange={change.start} />
      )}
    </CardFrame>
  );
}

type BodyProps = {
  health: Health;
  // A change is on its way: every switch waits, so two cannot cross.
  busy: boolean;
  failure: TransportError | undefined;
  onChange: (change: Change) => void;
};

function HealthBody({ health, busy, failure, onChange }: BodyProps) {
  const t = useTranslate();
  if (health.rows.length === 0) return <p>{t("health_empty")}</p>;

  const { ok, problem, off, unchecked } = health.counts;
  const summary = t("health_summary", { ok, problem, off });
  const switchableScheduler = health.rows.some((row) => row.name === "scheduler" && row.enabled !== undefined);
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
      {failure && <Problem error={failure} />}
      <ul className="services">
        {health.rows.map((row, index) => {
          const label = STATUS_IDS[row.status];
          return (
            <li key={row.name} data-tone={row.tone} style={{ "--i": index } as CSSProperties}>
              <span className="service-name">{row.name}</span>
              <span className="service-status">{label ? t(label) : row.status}</span>
              {row.detail && <span className="service-detail">{row.detail}</span>}
              {row.enabled !== undefined && (
                <Switch
                  label={t("switch_for", { name: row.name })}
                  on={row.enabled}
                  busy={busy}
                  onToggle={() => onChange({ type: "services.set", payload: { name: row.name, enabled: !row.enabled } })}
                />
              )}
            </li>
          );
        })}
      </ul>
      {switchableScheduler && <p className="note">{t("scheduler_hint")}</p>}
      {health.jobs.length > 0 && (
        <>
          <h3>{t("jobs_title")}</h3>
          <ul className="jobs">
            {health.jobs.map((job) => (
              <JobItem key={job.name} job={job} busy={busy} onChange={onChange} />
            ))}
          </ul>
        </>
      )}
    </>
  );
}

function Switch({ label, on, busy, onToggle }: { label: string; on: boolean; busy: boolean; onToggle: () => void }) {
  const t = useTranslate();
  return (
    <button type="button" role="switch" className="switch" aria-checked={on} aria-label={label} disabled={busy} onClick={onToggle}>
      {on ? t("switch_on") : t("switch_off")}
    </button>
  );
}

function JobItem({ job, busy, onChange }: { job: JobState; busy: boolean; onChange: (change: Change) => void }) {
  const t = useTranslate();
  const { ago } = useTimes();
  const start = intervalFields(job.interval);
  const [amount, setAmount] = useState(start.amount);
  const [unit, setUnit] = useState<IntervalUnit>(start.unit);
  const interval = intervalOf(amount, unit);
  const when = job.lastAt ? ago(job.lastAt) : "";

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (interval && !busy) onChange({ type: "scheduler.jobs.set", payload: { name: job.name, interval } });
  };

  return (
    <li data-enabled={job.enabled}>
      <span className="service-name">{job.name}</span>
      <Switch
        label={t("switch_for", { name: job.name })}
        on={job.enabled}
        busy={busy}
        onToggle={() => onChange({ type: "scheduler.jobs.set", payload: { name: job.name, enabled: !job.enabled } })}
      />
      <span className="service-detail">
        {[
          job.interval ? t("job_every", { interval: formatInterval(job.interval) }) : "",
          job.intervalOverride ? t("job_custom") : "",
          when ? t("job_last", { when, outcome: job.lastOutcome || "?" }) : t("job_never"),
        ]
          .filter((part) => part !== "")
          .join(" · ")}
      </span>
      {job.lastError && <span className="service-detail">{job.lastError}</span>}
      <form className="job-interval" onSubmit={submit}>
        <input
          type="number"
          inputMode="numeric"
          min={1}
          aria-label={t("job_interval_label", { name: job.name })}
          value={amount}
          disabled={busy}
          onChange={(event) => setAmount(event.target.value)}
        />
        <select aria-label={t("job_unit_label", { name: job.name })} value={unit} disabled={busy} onChange={(event) => setUnit(event.target.value as IntervalUnit)}>
          <option value="m">{t("unit_minutes")}</option>
          <option value="h">{t("unit_hours")}</option>
        </select>
        <button type="submit" disabled={busy || !interval}>
          {t("job_apply")}
        </button>
        {job.intervalOverride && (
          <button
            type="button"
            disabled={busy}
            onClick={() => onChange({ type: "scheduler.jobs.set", payload: { name: job.name, interval: "default" } })}
          >
            {t("job_reset")}
          </button>
        )}
      </form>
    </li>
  );
}
