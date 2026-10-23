import { useCallback, useEffect, useId, useState } from "react";
import type { ProviderRegistrationChange, ProviderRow, ProvidersView } from "../../shared/runtime.js";
import type { Transport } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame, Problem } from "./card_frame.js";
import { useAction } from "./use_action.js";
import { useCard, type LoadResult } from "./use_card.js";

// What inference the machine could offer, asked when the view opens (never before): discovery only,
// nothing is selected or started.
export function ProvidersCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(async (): Promise<LoadResult<ProvidersView>> => {
    const answer = await transport.request("runtime.providers", {}, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || !Array.isArray(answer.result.providers)) return { ok: false, error: { code: "unexpected", message: "unexpected provider inventory" } };
    return { ok: true, data: answer.result as ProvidersView };
  }, [transport]);
  const { state, reload } = useCard(load);
  const save = useCallback(async (change: ProviderRegistrationChange): Promise<LoadResult<true>> => {
    const answer = await transport.request("runtime.providers.configure", {
      configuration_revision: change.configurationRevision, provider: change.provider, enabled: change.enabled, models: change.models,
    }, { timeoutMs: 15_000 });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || answer.result.provider !== change.provider || answer.result.configurationRevision !== change.configurationRevision + 1) return { ok: false, error: { code: "unexpected", message: "incompatible provider save response; reload configuration" } };
    return { ok: true, data: true };
  }, [transport]);
  const action = useAction(save);
  useEffect(() => { if (action.state.phase === "ready") reload(); }, [action.state, reload]);
  return <CardFrame title={t("providers_title")} state={state} onReload={reload}>
    {(data) => <>
      <p className="detail">{t("providers_discovery_only")}</p>
      {action.state.phase === "error" && <Problem error={action.state.error} />}
      {!data.configurationRevision && <p className="detail">{t("provider_configuration_unavailable")}</p>}
      <ul className="services">{data.providers.map((row) => <li key={row.id}>
        <span className="service-name">{row.name}</span>
        <span className="service-status">{t(row.enabled === false ? "provider_disabled" : row.reason === "service_stopped" ? "provider_service_stopped" : row.state === "installed" ? "provider_installed" : row.state === "reachable" ? "provider_reachable" : "provider_unavailable")}</span>
        <span className="detail">{row.modelDiscovery === "listed"
          ? row.models.length ? row.models.join(", ") : t("provider_no_models")
          : t("provider_models_unavailable")}</span>
        {row.truncated && <span className="detail">{t("provider_models_truncated")}</span>}
        {data.configurationRevision ? <p className="detail">{t("provider_declared_models")}: {(row.declaredModels ?? []).join(", ") || t("provider_no_declarations")}</p> : null}
        <ProviderPreferences row={row} revision={data.configurationRevision ?? 0} busy={action.state.phase === "loading"} onSave={action.start} />
      </li>)}</ul>
    </>}
  </CardFrame>;
}

function ProviderPreferences({ row, revision, busy, onSave }: { row: ProviderRow; revision: number; busy: boolean; onSave: (change: ProviderRegistrationChange) => void }) {
  const t = useTranslate();
  const id = useId();
  const [draft, setDraft] = useState<{ revision: number; enabled: boolean; models: string }>();
  const [reviewing, setReviewing] = useState(false);
  if (!revision) return null;
  const close = () => { setDraft(undefined); setReviewing(false); };
  if (!draft) return <button type="button" disabled={busy} onClick={() => setDraft({ revision, enabled: row.enabled !== false, models: (row.declaredModels ?? []).join(", ") })} aria-label={t("provider_edit_named", { name: row.name })}>{t("setting_edit")}</button>;
  return <form className="setting-edit" onSubmit={(event) => { event.preventDefault(); setReviewing(true); }}>
    <label htmlFor={`${id}-enabled`}><input id={`${id}-enabled`} type="checkbox" checked={draft.enabled} disabled={busy} onChange={(event) => { setDraft({ ...draft, enabled: event.target.checked }); setReviewing(false); }} /> {t("provider_discovery_enabled")}</label>
    <label htmlFor={`${id}-models`}>{t("provider_declared_models")}</label>
    <input id={`${id}-models`} value={draft.models} maxLength={2200} autoComplete="off" disabled={busy} onChange={(event) => { setDraft({ ...draft, models: event.target.value }); setReviewing(false); }} />
    <p className="detail">{t("provider_models_hint")}</p>
    {!reviewing ? <><button type="submit" disabled={busy}>{t("setting_review")}</button><button type="button" disabled={busy} onClick={close}>{t("runtime_confirm_cancel")}</button></>
      : <div className="module-confirm" role="alertdialog" aria-label={t("provider_save_named", { name: row.name })}>
        <p>{t("provider_registration_notice")}</p>
        <p>{t(draft.enabled ? "provider_discovery_enabled" : "provider_disabled")}: {draft.models || t("provider_no_declarations")}</p>
        <button type="button" disabled={busy} onClick={() => onSave({ provider: row.id, configurationRevision: draft.revision, enabled: draft.enabled, models: draft.models.split(",").map((name) => name.trim()).filter(Boolean) })}>{t("provider_save_registration")}</button>
        <button type="button" disabled={busy} onClick={close}>{t("runtime_confirm_cancel")}</button>
      </div>}
  </form>;
}
