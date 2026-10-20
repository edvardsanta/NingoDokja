import { useCallback, useEffect, useRef, useState, type CSSProperties, type FormEvent, type ReactNode } from "react";

import type { JobState } from "../../shared/replies.js";
import type { Transport, TransportError, TransportResult } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { usePulse } from "../pulse.js";
import { CardFrame, Problem } from "./card_frame.js";
import {
  formatInterval,
  intervalFields,
  intervalOf,
  parseHealth,
  parseLink,
  pulseOf,
  tonesOf,
  type Health,
  type LinkView,
  type IntervalUnit,
  type ServiceRow,
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

// What an interface says about itself, from the closed list each one reports. A state this screen
// does not know is shown as it came.
const STATE_IDS: Record<string, MessageId> = {
  connected: "state_connected",
  connecting: "state_connecting",
  disconnected: "state_disconnected",
  listening: "state_listening",
  waiting_for_link: "state_waiting_for_link",
  logged_out: "state_logged_out",
  replaced: "state_replaced",
  refused: "state_refused",
  unreachable: "state_unreachable",
  off: "state_off",
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

// The interface the desktop links. The shell fixes the name again before anything is sent.
const LINKABLE = "whatsapp";
const LINK_POLL_MS = 2000;
// Unlinking asks WhatsApp to drop the device first, which can take a while.
const LINK_RESET_TIMEOUT_MS = 45_000;

export function HealthCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const { report, reportTones } = usePulse();
  const load = useCallback(() => loadHealth(transport), [transport]);
  const { state, reload } = useCard(load);
  const run = useCallback((change: Change) => applyChange(transport, change), [transport]);
  const change = useAction(run);
  const [failure, setFailure] = useState<TransportError>();

  useEffect(() => {
    report(pulseOf(state));
    // while a reload runs, the sidebar keeps the tones it has instead of blinking them away
    if (state.phase !== "loading") reportTones(tonesOf(state));
  }, [state, report, reportTones]);

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
  const link = useLink(transport, reload);

  return (
    <CardFrame kindLabel={t("kind_health")} title={t("health_title")} state={state} onReload={refresh}>
      {(health) => (
        <HealthBody
          health={health}
          busy={change.state.phase === "loading"}
          failure={failure}
          onChange={change.start}
          link={link}
        />
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
  link: Link;
};

function HealthBody({ health, busy, failure, onChange, link }: BodyProps) {
  const t = useTranslate();
  if (health.rows.length === 0 && health.interfaces.length === 0) return <p>{t("health_empty")}</p>;

  const { ok, problem, off, unchecked } = health.counts;
  const summary = t("health_summary", { ok, problem, off });
  const switchableScheduler = health.rows.some((row) => row.name === "scheduler" && row.enabled !== undefined);
  return (
    <>
      <div className="strip" aria-hidden="true">
        {[...health.rows, ...health.interfaces].map((row) => (
          <span key={row.name} data-tone={row.tone} />
        ))}
      </div>
      <p className="summary">
        {unchecked > 0 ? `${summary}, ${t("health_unchecked", { count: unchecked })}` : summary}
      </p>
      {failure && <Problem error={failure} />}
      <ul className="services">
        {health.rows.map((row, index) => (
          <ServiceRowItem key={row.name} row={row} index={index} busy={busy} onChange={onChange} />
        ))}
      </ul>
      {health.interfaces.length > 0 && (
        <>
          <h3>{t("interfaces_title")}</h3>
          <ul className="services interfaces">
            {health.interfaces.map((row, index) => (
              <ServiceRowItem
                key={row.name}
                row={row}
                index={health.rows.length + index}
                busy={busy}
                onChange={onChange}
                extra={
                  row.name === LINKABLE ? <LinkControl link={link} /> : undefined
                }
              />
            ))}
          </ul>
          <p className="note">{t("interfaces_hint")}</p>
        </>
      )}
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

type RowProps = {
  row: ServiceRow;
  index: number;
  busy: boolean;
  onChange: (change: Change) => void;
  // Whatever an interface needs beyond its switch, shown across the row (the link panel).
  extra?: ReactNode;
};

function ServiceRowItem({ row, index, busy, onChange, extra }: RowProps) {
  const t = useTranslate();
  // An interface says its state; a switched-off one has none and says only that it is off.
  const stateId = row.state ? STATE_IDS[row.state] : undefined;
  const label = stateId ?? STATUS_IDS[row.status];
  return (
    <li data-tone={row.tone} data-kind={row.kind} style={{ "--i": index } as CSSProperties}>
      <span className="service-name">{row.name}</span>
      <span className="service-status">{label ? t(label) : row.state || row.status}</span>
      {row.detail && <span className="service-detail">{row.detail}</span>}
      {row.enabled !== undefined && (
        <Switch
          label={t("switch_for", { name: row.name })}
          on={row.enabled}
          busy={busy}
          onToggle={() => onChange({ type: "services.set", payload: { name: row.name, enabled: !row.enabled } })}
        />
      )}
      {extra}
    </li>
  );
}

type LinkAction = "communications.link.start" | "communications.link.reset";

// The state of the link panel. It lives in the card, above its body: reading the status again
// replaces the body, and a panel that closed itself every time the account changed (or a new code
// was asked for) would never show the code it was opened for.
//
// While the panel is open it reads the state of the link every couple of seconds. The code only
// ever arrives as the picture the shell drew; asking for a new one, or starting over, is a change
// the shell lets through just after a click, and starting over is asked in a window of its own.
function useLink(transport: Transport, onChanged: () => void) {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<LinkView>();
  const [failure, setFailure] = useState<TransportError>();
  const [busy, setBusy] = useState(false);
  const lastState = useRef<string | undefined>(undefined);

  const apply = useCallback(
    (answer: TransportResult) => {
      if (!answer.ok) {
        // Saying no to the window is an answer, not a failure.
        setFailure(answer.error.code === "cancelled" ? undefined : answer.error);
        return;
      }
      const next = parseLink(answer.result);
      if (!next) {
        setFailure({ code: "unexpected", message: "unexpected link reply" });
        return;
      }
      setFailure(undefined);
      setView(next);
      if (lastState.current !== undefined && lastState.current !== next.state) onChanged();
      lastState.current = next.state;
    },
    [onChanged],
  );

  useEffect(() => {
    if (!open) return undefined;
    let cancelled = false;
    const read = async () => {
      if (document.visibilityState === "hidden") return;
      const answer = await transport.request("communications.link.status", { interface: LINKABLE }, { timeoutMs: 8000 });
      if (!cancelled) apply(answer);
    };
    void read();
    const timer = setInterval(() => void read(), LINK_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [open, transport, apply]);

  const toggle = useCallback(() => {
    setOpen((current) => {
      if (current) {
        // A code that was on screen is not worth showing again when the panel is reopened.
        setView(undefined);
        setFailure(undefined);
        lastState.current = undefined;
      }
      return !current;
    });
  }, []);

  const run = useCallback(
    async (type: LinkAction) => {
      setBusy(true);
      const timeoutMs = type === "communications.link.reset" ? LINK_RESET_TIMEOUT_MS : 10_000;
      const answer = await transport.request(type, { interface: LINKABLE }, { timeoutMs });
      setBusy(false);
      apply(answer);
      onChanged();
    },
    [transport, apply, onChanged],
  );

  return { open, toggle, view, failure, busy, run };
}

type Link = ReturnType<typeof useLink>;

// The button that opens the panel to link the account, and the panel.
function LinkControl({ link }: { link: Link }) {
  const t = useTranslate();
  return (
    <>
      <button type="button" className="link-open" aria-expanded={link.open} onClick={link.toggle}>
        {link.open ? t("link_close") : t("link_open")}
      </button>
      {link.open && <LinkPanel link={link} />}
    </>
  );
}

function LinkPanel({ link }: { link: Link }) {
  const t = useTranslate();
  const { view, failure, busy, run } = link;
  const state = view?.state;
  const canAskForCode = state === "waiting_for_link" || state === "refused" || state === "connecting";
  return (
    <div className="link-panel" aria-busy={busy}>
      <h4>{t("link_title")}</h4>
      {failure && <Problem error={failure} />}
      {state === "connected" ? (
        <p className="note" data-tone="ok">
          {t("link_done")}
        </p>
      ) : view?.image ? (
        <>
          <p>{t("link_steps")}</p>
          <img className="link-code" src={view.image} alt={t("link_code_alt")} width={232} height={232} />
        </>
      ) : (
        <p role="status">{busy ? t("link_working") : t("link_waiting")}</p>
      )}
      <div className="link-actions">
        {canAskForCode && (
          <button type="button" disabled={busy} onClick={() => void run("communications.link.start")}>
            {t("link_new")}
          </button>
        )}
        <button type="button" disabled={busy} onClick={() => void run("communications.link.reset")}>
          {t("link_unlink")}
        </button>
      </div>
    </div>
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
