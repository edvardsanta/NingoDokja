import { useCallback, useEffect } from "react";

import { isRecord } from "../../shared/records.js";
import { RUNTIME_MODULES, type RuntimeModule, type RuntimeSnapshot, type RuntimeState } from "../../shared/runtime.js";
import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { CardFrame, Problem } from "./card_frame.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

const STATES: Record<RuntimeState, MessageId> = {
  starting: "runtime_starting",
  ready: "runtime_ready",
  running: "runtime_running",
  stopping: "runtime_stopping",
  stopped: "runtime_stopped",
  failed: "runtime_failed",
};

type Change = { module: RuntimeModule; enabled: boolean };

export function RuntimeCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(async (): Promise<LoadResult<RuntimeSnapshot>> => {
    const result = await transport.request("runtime.modules", {}, { timeoutMs: 15_000 });
    if (!result.ok) return result;
    if (!isRecord(result.result) || !Array.isArray(result.result.modules)) return unexpected();
    return { ok: true, data: result.result as RuntimeSnapshot };
  }, [transport]);
  const { state, reload } = useCard(load);
  const run = useCallback(async (choice: Change): Promise<LoadResult<RuntimeSnapshot>> => {
    const answer = await transport.request("runtime.set", choice, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || !Array.isArray(answer.result.modules)) return unexpected();
    return { ok: true, data: answer.result as RuntimeSnapshot };
  }, [transport]);
  const action = useAction(run);
  const snapshot = state.phase === "ready" ? state.data : undefined;
  const busy = snapshot?.busy || action.state.phase === "loading";

  // An acknowledgement starts a background operation; it is not readiness. Read
  // the controller again after both success and failure, then poll while busy.
  useEffect(() => {
    if (action.state.phase === "ready" || action.state.phase === "error") reload();
  }, [action.state, reload]);
  useEffect(() => {
    if (!snapshot?.busy) return;
    const timer = setTimeout(reload, 1200);
    return () => clearTimeout(timer);
  }, [snapshot, reload]);

  return <CardFrame kindLabel={t("nav_system")} title={t("runtime_title")} state={state} onReload={reload}>
    {(view) => <>
      <p>{t("runtime_intro")}</p>
      {view.error && <p role="alert">{view.error}</p>}
      {action.state.phase === "error" && <Problem error={action.state.error} />}
      <ul className="services">{view.modules.filter((module) => RUNTIME_MODULES.includes(module.name)).map((module) => <li key={module.name}>
        <strong>{module.name}</strong> · <span>{view.error ? t("runtime_unknown") : t(STATES[module.state])}</span>{" "}
        <button type="button" disabled={!!busy || !!view.error}
          aria-label={t("runtime_start_named", { name: module.name })}
          onClick={() => action.start({ module: module.name, enabled: true })}>{t("runtime_start")}</button>{" "}
        <button type="button" disabled={!!busy || !!view.error || module.state === "stopped"}
          aria-label={t("runtime_stop_named", { name: module.name })}
          onClick={() => action.start({ module: module.name, enabled: false })}>{t("runtime_stop")}</button>
        {module.error && <p role="alert">{module.error}</p>}
      </li>)}</ul>
      <p className="detail">{t("runtime_scope")}</p>
    </>}
  </CardFrame>;
}

function unexpected(): LoadResult<never> {
  return { ok: false, error: { code: "unexpected", message: "unexpected runtime response" } };
}
