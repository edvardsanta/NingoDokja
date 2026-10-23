import { useCallback, useEffect, useId, useState } from "react";
import type { ChatBinding, ProviderRegistrationChange, ProviderRow, ProvidersView } from "../../shared/runtime.js";
import type { Transport } from "../../shared/transport.js";
import { isRecord } from "../../shared/records.js";
import { useTranslate } from "../i18n/context.js";
import { CardFrame, Problem } from "./card_frame.js";
import { availableProviderModels } from "./provider_model_catalog.js";
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
  const [current, setCurrent] = useState<ChatBinding>();
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
  // Choosing a model for chat is one press: declare it in its provider's registration when it is not
  // yet, then select it. The revision is read again between the two, since the first write moves it.
  const [chatRound, setChatRound] = useState(0);
  const use = useCallback(async (choice: { row: ProviderRow; model: string; revision: number }): Promise<LoadResult<true>> => {
    const { row, model, revision } = choice;
    const declared = row.declaredModels ?? [];
    if (row.enabled === false || !declared.includes(model)) {
      const saved = await save({ provider: row.id, configurationRevision: revision, enabled: true, models: [...declared, model].slice(-16) });
      if (!saved.ok) return saved;
    }
    const chat = await transport.request("runtime.chat", {});
    if (!chat.ok) return chat;
    if (!isRecord(chat.result) || typeof chat.result.configurationRevision !== "number") return { ok: false, error: { code: "unexpected", message: "unexpected chat configuration" } };
    const current = chat.result.configurationRevision;
    const answer = await transport.request("runtime.chat.configure", { configuration_revision: current, provider: row.id, model });
    if (!answer.ok) return answer;
    if (!isRecord(answer.result) || answer.result.provider !== row.id || answer.result.model !== model) return { ok: false, error: { code: "unexpected", message: "incompatible chat configuration response" } };
    return { ok: true, data: true };
  }, [transport, save]);
  const using = useAction(use);
  useEffect(() => {
    if (using.state.phase !== "ready") return;
    reload();
    setChatRound((value) => value + 1);
  }, [using.state, reload]);
  return <CardFrame title={t("providers_title")} state={state} onReload={reload}>
    {(data) => <>
      {data.configurationRevision ? <ChatSelection key={chatRound} transport={transport} providers={data.providers} onCurrent={setCurrent} /> : null}
      <p className="detail">{t("providers_discovery_only")}</p>
      {action.state.phase === "error" && <Problem error={action.state.error} />}
      {using.state.phase === "error" && <Problem error={using.state.error} />}
      {!data.configurationRevision && <p className="detail">{t("provider_configuration_unavailable")}</p>}
      <ul className="services provider-list">{data.providers.map((row) => <li key={row.id}>
        <span className="service-name">{row.name}</span>
        <span className="service-status">{t(row.enabled === false ? "provider_disabled" : row.reason === "service_stopped" ? "provider_service_stopped" : row.state === "installed" ? "provider_installed" : row.state === "reachable" ? "provider_reachable" : "provider_unavailable")}</span>
        <span className="detail">{row.modelDiscovery === "listed"
          ? row.models.length ? row.models.join(", ") : t("provider_no_models")
          : t("provider_models_unavailable")}</span>
        {row.truncated && <span className="detail">{t("provider_models_truncated")}</span>}
        {data.configurationRevision && availableProviderModels(row).length > 0 ? <ul className="provider-models">{availableProviderModels(row).map((model) => {
          const inUse = current?.provider === row.id && current.model === model;
          return <li key={model}>
            <span>{model}</span>
            {inUse ? <span className="provider-in-use">{t("provider_in_use")}</span>
              : <button type="button" disabled={using.state.phase === "loading" || action.state.phase === "loading"}
                aria-label={t("provider_use_named", { model, name: row.name })}
                onClick={() => using.start({ row, model, revision: data.configurationRevision ?? 0 })}>{t("provider_use")}</button>}
          </li>;
        })}</ul> : null}
        {data.configurationRevision ? <details className="more provider-advanced">
          <summary>{t("provider_advanced")}</summary>
          <p className="detail">{t("provider_declared_models")}: {(row.declaredModels ?? []).join(", ") || t("provider_no_declarations")}</p>
          <ProviderPreferences row={row} revision={data.configurationRevision ?? 0} busy={action.state.phase === "loading"} onSave={action.start} />
        </details> : null}
      </li>)}</ul>
    </>}
  </CardFrame>;
}

function ProviderPreferences({ row, revision, busy, onSave }: { row: ProviderRow; revision: number; busy: boolean; onSave: (change: ProviderRegistrationChange) => void }) {
  const t = useTranslate();
  const id = useId();
  const [draft, setDraft] = useState<{ revision: number; enabled: boolean; models: string[] }>();
  const [reviewing, setReviewing] = useState(false);
  const [candidate, setCandidate] = useState("");
  const available = availableProviderModels(row).filter((model) => !draft?.models.includes(model));
  if (!revision) return null;
  const close = () => { setDraft(undefined); setReviewing(false); };
  if (!draft) return <button type="button" disabled={busy} onClick={() => setDraft({ revision, enabled: row.enabled !== false, models: row.declaredModels ?? [] })} aria-label={t("provider_edit_named", { name: row.name })}>{t("setting_edit")}</button>;
  return <form className="setting-edit" onSubmit={(event) => { event.preventDefault(); setReviewing(true); }}>
    <label htmlFor={`${id}-enabled`}><input id={`${id}-enabled`} type="checkbox" checked={draft.enabled} disabled={busy} onChange={(event) => { setDraft({ ...draft, enabled: event.target.checked }); setReviewing(false); }} /> {t("provider_discovery_enabled")}</label>
    <div className="list-editor">
      <label htmlFor={`${id}-model`}>{t("provider_declared_models")}</label>
      {draft.models.length ? <ul className="list-editor-items">{draft.models.map((model) => <li key={model}>
        <span>{model}</span>
        <button type="button" disabled={busy} aria-label={t("list_remove_named", { name: model })} onClick={() => { setDraft({ ...draft, models: draft.models.filter((item) => item !== model) }); setReviewing(false); }}>{t("list_remove")}</button>
      </li>)}</ul> : <p className="detail list-editor-empty">{t("setting_none")}</p>}
      <div className="list-editor-add">
        <select id={`${id}-model`} value={candidate} disabled={busy || available.length === 0 || draft.models.length >= 16} onChange={(event) => setCandidate(event.target.value)}>
          <option value="">{t("provider_choose_model")}</option>
          {available.map((model) => <option key={model} value={model}>{model}</option>)}
        </select>
        <button type="button" disabled={busy || !candidate || !available.includes(candidate) || draft.models.length >= 16} onClick={() => { setDraft({ ...draft, models: [...draft.models, candidate] }); setCandidate(""); setReviewing(false); }}>{t("list_add")}</button>
      </div>
    </div>
    <p className="detail">{t(row.id === "ollama" ? "provider_models_ollama_hint" : "provider_models_cli_hint")}</p>
    {!reviewing ? <><button type="submit" disabled={busy}>{t("setting_review")}</button><button type="button" disabled={busy} onClick={close}>{t("runtime_confirm_cancel")}</button></>
      : <div className="module-confirm" role="alertdialog" aria-label={t("provider_save_named", { name: row.name })}>
        <p>{t("provider_registration_notice")}</p>
        <p>{t(draft.enabled ? "provider_discovery_enabled" : "provider_disabled")}: {draft.models.join(", ") || t("provider_no_declarations")}</p>
        <button type="button" disabled={busy} onClick={() => onSave({ provider: row.id, configurationRevision: draft.revision, enabled: draft.enabled, models: draft.models })}>{t("provider_save_registration")}</button>
        <button type="button" disabled={busy} onClick={close}>{t("runtime_confirm_cancel")}</button>
      </div>}
  </form>;
}

function ChatSelection({transport,providers,onCurrent}:{transport:Transport;providers:ProviderRow[];onCurrent:(binding:ChatBinding)=>void}) {
 const t=useTranslate();
 const load=useCallback(async ():Promise<LoadResult<import("../../shared/runtime.js").ChatBinding>>=>{
 const result=await transport.request("runtime.chat",{});if(!result.ok)return result;
 return {ok:true,data:result.result as import("../../shared/runtime.js").ChatBinding};},[transport]);
 const {state,reload}=useCard(load);
 useEffect(()=>{if(state.phase==="ready")onCurrent(state.data);},[state,onCurrent]);
 const [draft,setDraft]=useState<{revision:number;provider:string;model:string}>();
 const save=useAction(useCallback(async (value:{revision:number;provider:string;model:string}):Promise<LoadResult<true>>=>{
 const result=await transport.request("runtime.chat.configure",{configuration_revision:value.revision,provider:value.provider,model:value.model});if(!result.ok)return result;
 if(!isRecord(result.result)||result.result.configurationRevision!==value.revision+1||result.result.provider!==value.provider||result.result.model!==value.model)return {ok:false,error:{code:"unexpected",message:"incompatible chat configuration response"}};
 return {ok:true,data:true};},[transport]));
 useEffect(()=>{if(save.state.phase==="ready"){setDraft(undefined);reload();}},[save.state,reload]);
 return <section aria-label={t("chat_provider_title")}>
 <h3>{t("chat_provider_title")}</h3><p>{t("chat_provider_hint")}</p>
 {state.phase==="error"&&<Problem error={state.error}/>}
 {save.state.phase==="error"&&<Problem error={save.state.error}/>}
 {state.phase==="ready"&&<>
 <p>{state.data.provider ? `${state.data.provider} / ${state.data.model}`:t("chat_provider_missing")}</p>
 {!draft?<button type="button" onClick={()=>setDraft({revision:state.data.configurationRevision,provider:state.data.provider,model:state.data.model})}>{t("setting_edit")}</button>:
 <form onSubmit={e=>{e.preventDefault();save.start(draft);}}>
 <label>{t("chat_provider_title")}<select value={draft.provider+"|"+draft.model} disabled={save.state.phase==="loading"} onChange={e=>{const [provider,model]=e.target.value.split("|");setDraft({...draft,provider:provider??"",model:model??""});}}>
 <option value="|">{t("chat_provider_missing")}</option>
 {providers.filter(p=>p.enabled!==false).flatMap(p=>(p.declaredModels??[]).map(model=><option key={p.id+"|"+model} value={p.id+"|"+model}>{p.name} / {model}</option>))}
 </select></label>
 <button type="submit" disabled={!draft.provider||!draft.model||save.state.phase==="loading"}>{t("chat_provider_save")}</button>
 <button type="button" disabled={save.state.phase==="loading"} onClick={()=>setDraft(undefined)}>{t("runtime_confirm_cancel")}</button>
 </form>}
 </>}
 </section>;
}
