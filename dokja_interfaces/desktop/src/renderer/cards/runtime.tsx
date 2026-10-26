import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { isRecord } from "../../shared/records.js";
import { DELIVERY_MODULES, RUNTIME_MODULES, type RuntimeModule, type RuntimeSnapshot, type RuntimeState } from "../../shared/runtime.js";
import type { Transport } from "../../shared/transport.js";
import { displayName } from "../display_names.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { CardFrame, Problem } from "./card_frame.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";
import { withoutLinkCodes } from "./log_text.js";

const STATES: Record<RuntimeState, MessageId> = {
  starting: "runtime_starting",
  ready: "runtime_ready",
  running: "runtime_running",
  stopping: "runtime_stopping",
  stopped: "runtime_stopped",
  failed: "runtime_failed",
};

type Change = { module: RuntimeModule; enabled: boolean };

type Props = {
  transport: Transport;
  moduleDetails?: (name: RuntimeModule) => ReactNode;
  onChanged?: () => void;
  renderActivity?: (runtimeAvailable: boolean) => ReactNode;
  // What starting a delivery module will do, from the switches and jobs already saved; undefined
  // when they are unknown.
  startNotice?: (name: RuntimeModule) => string[] | undefined;
};

export function RuntimeCard({ transport, moduleDetails, onChanged, renderActivity, startNotice }: Props) {
  const t = useTranslate();
  const [asking, setAsking] = useState<RuntimeModule>();
  const load = useCallback(async (): Promise<LoadResult<RuntimeSnapshot>> => {
    const result = await transport.request("runtime.modules", {}, { timeoutMs: 15_000 });
    if (!result.ok) return result;
    if (!isRecord(result.result) || !Array.isArray(result.result.modules)) return unexpected();
    return { ok: true, data: result.result as RuntimeSnapshot };
  }, [transport]);
  const { state, poll } = useCard(load);
  const run = useCallback(async (choice: Change): Promise<LoadResult<RuntimeSnapshot>> => {
    const answer = await transport.request("runtime.set", choice, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || !Array.isArray(answer.result.modules)) return unexpected();
    return { ok: true, data: answer.result as RuntimeSnapshot };
  }, [transport]);
  const action = useAction(run);
  const snapshot = state.phase === "ready" ? state.data : undefined;
  const busy = action.state.phase === "loading";

  const wasBusy = useRef(false);
  useEffect(() => {
    if (!snapshot) return;
    if (wasBusy.current && !snapshot.busy) onChanged?.();
    wasBusy.current = snapshot.busy;
  }, [snapshot, onChanged]);
  const refresh = useCallback(() => { poll(); onChanged?.(); }, [poll, onChanged]);

  // An acknowledgement starts a background operation; it is not readiness. Read
  // the controller again after both success and failure, then poll while busy.
  useEffect(() => {
    if (action.state.phase === "ready" || action.state.phase === "error") refresh();
  }, [action.state, refresh]);
  useEffect(() => {
    if (!snapshot?.busy) return;
    const timer = setTimeout(poll, 1200);
    return () => clearTimeout(timer);
  }, [snapshot, poll]);

  return <CardFrame title={t("runtime_title")} state={state} onReload={refresh} supplement={renderActivity?.(state.phase !== "error")}>
    {(view) => <>
      <p className="detail">{t("runtime_intro")}</p>
      {view.error && <p role="alert">{view.error}</p>}
      {action.state.phase === "error" && <Problem error={action.state.error} />}
      <ul className="services runtime-modules">{view.modules.filter((module) => RUNTIME_MODULES.includes(module.name)).map((module) => {
        const start = module.state === "stopped" || module.state === "failed";
        const details = moduleDetails?.(module.name);
        const label = displayName(module.name, t);
        const moving = module.state === "starting" || module.state === "stopping";
        return <li key={module.name} data-tone={module.state === "failed" ? "problem" : start ? "off" : moving ? "progress" : "ok"}>
          <span className="service-name" title={module.name}>{label}</span>
          <span className="service-status">{view.error ? t("runtime_unknown") : t(STATES[module.state])}</span>
          <button type="button" className="switch" disabled={!!busy || !!view.error || module.state === "starting" || module.state === "stopping"}
            aria-label={t(start ? "runtime_start_named" : "runtime_stop_named", { name: label })}
            onClick={() => start && DELIVERY_MODULES.includes(module.name) ? setAsking(module.name) : action.start({ module: module.name, enabled: start })}>
            {t(start ? "runtime_start" : "runtime_stop")}
          </button>
          {module.progress && <p className="module-progress">{progressText(module.progress, t)}</p>}
          <RuntimeLogs transport={transport} name={module.name} label={label} />
          {asking === module.name && start && <div className="module-confirm" role="alertdialog" aria-label={t("runtime_confirm_title", { name: label })}>
            <p>{t("runtime_confirm_title", { name: label })}</p>
            <ul>{(startNotice?.(module.name) ?? [t("delivery_status_unknown")]).map((line) => <li key={line}>{line}</li>)}</ul>
            <button type="button" disabled={!!busy || !!view.error} onClick={() => { setAsking(undefined); action.start({ module: module.name, enabled: true }); }}>{t("runtime_confirm_start")}</button>
            <button type="button" onClick={() => setAsking(undefined)}>{t("runtime_confirm_cancel")}</button>
          </div>}
          {module.error && <p className="module-error" role="alert">{module.error}</p>}
          {(details || module.state === "failed") && <details className="module-options">
            <summary>{t("runtime_options", { name: label })}</summary>
            {details}
            {module.state === "failed" && <button type="button" disabled={!!busy || !!view.error}
              aria-label={t("runtime_stop_named", { name: label })}
              onClick={() => action.start({ module: module.name, enabled: false })}>{t("runtime_stop")}</button>}
          </details>}
        </li>;
      })}</ul>
      <p className="detail">{t("runtime_scope")}</p>
    </>}
  </CardFrame>;
}

function unexpected(): LoadResult<never> {
  return { ok: false, error: { code: "unexpected", message: "unexpected runtime response" } };
}

function progressText(progress: string, t: ReturnType<typeof useTranslate>): string {
  const [stage, name] = progress.split(":");
  const messages: Record<string, MessageId> = { queued: "runtime_queued", waiting: "runtime_waiting", starting: "runtime_building", readiness: "runtime_readiness", stopping: "runtime_stopping_dependency" };
  return t(messages[stage ?? "queued"] ?? "runtime_queued", { name: name ?? "" });
}

function RuntimeLogs({ transport, name, label }: { transport: Transport; name: RuntimeModule; label: string }) {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  const [output, setOutput] = useState("");
  const [error, setError] = useState(false);
  const box = useRef<HTMLPreElement>(null);
  const follow = useRef(true);
  useEffect(() => {
    if (!open) return;
    let current = true;
    let timer: ReturnType<typeof setTimeout>;
    const read = async () => {
      try {
        const answer = await transport.request("runtime.logs", { module: name }, { timeoutMs: 8000 });
        if (!current) return;
        if (answer.ok && isRecord(answer.result) && answer.result.module === name && typeof answer.result.text === "string") {
          setOutput(answer.result.text);
          setError(false);
        } else setError(true);
      } catch { if (current) setError(true); }
      if (current) timer = setTimeout(read, 2000);
    };
    void read();
    return () => { current = false; clearTimeout(timer); };
  }, [open, transport, name]);
  useEffect(() => {
    if (follow.current && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [output]);
  return <details className="runtime-logs" onToggle={(event) => setOpen(event.currentTarget.open)}>
    <summary>{t("runtime_logs", { name: label })}</summary>
    {open && <>
      <p className="detail">{t("runtime_logs_hint")}</p>
      {error && <p role="status">{t("runtime_logs_error")}</p>}
      <pre ref={box} tabIndex={0} aria-label={t("runtime_logs", { name: label })}
        onScroll={() => { const node = box.current; if (node) follow.current = node.scrollHeight - node.scrollTop - node.clientHeight < 40; }}>
        {output ? withoutLinkCodes(output, t("runtime_logs_link_code")) : t("runtime_logs_empty")}
      </pre>
    </>}
  </details>;
}
