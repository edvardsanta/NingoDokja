import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { ModelsStatus, ProviderRow } from "../../shared/runtime.js";
import type { Transport } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";
import { Problem } from "./card_frame.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

const MODEL_NAME = /^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$/;

// The local engine's own controls, under the Ollama row: start it when it is stopped, download a model
// by name and watch it come, and remove one. The shell asks before a download or a removal, in a
// window of its own; this panel only asks the controller. onChanged reloads the inventory.
export function LocalModels({ transport, row, onChanged }: { transport: Transport; row: ProviderRow; onChanged: () => void }) {
  const t = useTranslate();
  const id = useId();
  const load = useCallback(async (): Promise<LoadResult<ModelsStatus>> => {
    const answer = await transport.request("runtime.models.status", {});
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || typeof answer.result.embeddingModel !== "string") return { ok: false, error: { code: "unexpected", message: "unexpected models status" } };
    return { ok: true, data: answer.result as ModelsStatus };
  }, [transport]);
  const { state, poll } = useCard(load);
  const ask = useCallback(async (request: { type: "runtime.models.engine.start" | "runtime.models.pull" | "runtime.models.cancel"; model?: string }): Promise<LoadResult<true>> => {
    const answer = await transport.request(request.type, request.model ? { model: request.model } : {}, { timeoutMs: 15_000 });
    return answer.ok ? { ok: true, data: true } : answer;
  }, [transport]);
  const action = useAction(ask);
  const [name, setName] = useState("");
  useEffect(() => { if (action.state.phase === "ready" || action.state.phase === "error") poll(); }, [action.state, poll]);

  // While the engine starts or a model downloads, read again every second; when it ends, the
  // inventory is read again so the row shows what the engine now has.
  const status = state.phase === "ready" ? state.data : undefined;
  const watching = status?.pull?.state === "pulling" || status?.engine === "starting";
  const wasWatching = useRef(false);
  useEffect(() => {
    if (watching) {
      wasWatching.current = true;
      const timer = setTimeout(poll, 1000);
      return () => clearTimeout(timer);
    }
    if (wasWatching.current) {
      wasWatching.current = false;
      onChanged();
    }
    return undefined;
  }, [watching, status, poll, onChanged]);

  if (state.phase === "error") {
    // An older controller does not know these actions: the panel says nothing rather than fail.
    return state.error.code === "orchestrator" && /unsupported/.test(state.error.message) ? null : <Problem error={state.error} />;
  }
  if (!status) return null;
  const busy = action.state.phase === "loading";
  const stopped = row.reason === "service_stopped";
  const pull = status.pull;
  const valid = MODEL_NAME.test(name.trim());
  return <div className="local-models">
    {action.state.phase === "error" && action.state.error.code !== "cancelled" && <Problem error={action.state.error} />}
    {status.engine === "failed" && <p className="detail">{t("models_engine_failed")}{status.engineError ? `: ${status.engineError}` : ""}</p>}
    {stopped && status.engine !== "starting" && <p className="detail">
      {t("models_engine_stopped")}{" "}
      <button type="button" disabled={busy} onClick={() => action.start({ type: "runtime.models.engine.start" })}>{t("models_engine_start")}</button>
    </p>}
    {status.engine === "starting" && <p role="status">{t("models_engine_starting")}</p>}
    {pull?.state === "pulling" && <p role="status" className="model-pull">
      <span>{t("models_pull_progress", { model: pull.model, phase: t(pull.phase === "downloading" ? "models_phase_downloading" : pull.phase === "verifying" ? "models_phase_verifying" : pull.phase === "writing" ? "models_phase_writing" : "models_phase_starting") })}</span>
      {pull.percent !== undefined && <progress max={100} value={pull.percent} aria-label={t("models_pull_share", { model: pull.model })} />}
      {pull.percent !== undefined && <span>{pull.percent}%</span>}
      <button type="button" disabled={busy} onClick={() => action.start({ type: "runtime.models.cancel" })}>{t("models_pull_cancel")}</button>
    </p>}
    {pull?.state === "done" && <p role="status">{t("models_pull_done", { model: pull.model })}</p>}
    {pull?.state === "cancelled" && <p role="status">{t("models_pull_cancelled", { model: pull.model })}</p>}
    {pull?.state === "failed" && <p role="alert">{t("models_pull_failed", { model: pull.model })}{pull.error ? `: ${pull.error}` : ""}</p>}
    {!stopped && pull?.state !== "pulling" && <form className="model-download" onSubmit={(event) => {
      event.preventDefault();
      if (valid) action.start({ type: "runtime.models.pull", model: name.trim() });
    }}>
      <label htmlFor={`${id}-model`}>{t("models_download_label")}</label>
      <input id={`${id}-model`} value={name} maxLength={128} spellCheck={false} autoComplete="off" placeholder={t("models_download_placeholder")} disabled={busy} onChange={(event) => setName(event.target.value)} />
      <button type="submit" disabled={busy || !valid}>{t("models_download")}</button>
      <p className="detail">{t("models_download_hint")}</p>
    </form>}
  </div>;
}

// Removing a model, asked in the shell's window. Reloads the inventory when it is gone.
export function useRemoveModel(transport: Transport, onChanged: () => void) {
  const remove = useCallback(async (model: string): Promise<LoadResult<true>> => {
    const answer = await transport.request("runtime.models.remove", { model }, { timeoutMs: 15_000 });
    return answer.ok ? { ok: true, data: true } : answer;
  }, [transport]);
  const action = useAction(remove);
  useEffect(() => { if (action.state.phase === "ready") onChanged(); }, [action.state, onChanged]);
  return action;
}
