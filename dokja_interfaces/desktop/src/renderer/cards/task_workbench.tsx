import { useCallback, useEffect, useId, useState } from "react";
import { isRecord } from "../../shared/records.js";
import type { Transport } from "../../shared/transport.js";
import type { ProviderRow, TaskChoice, TaskDiagnostics, TaskProfiles } from "../../shared/runtime.js";
import { type MessageId } from "../i18n/i18n.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import { CardFrame, Problem } from "./card_frame.js";
import { useCard, type LoadResult } from "./use_card.js";
import { useAction } from "./use_action.js";
import { Button } from "./ui.js";

const TASK_MESSAGES:readonly MessageId[]=["tasks_reason_queue_full","tasks_profile_lightweight", "tasks_profile_analysis", "tasks_outcome_success", "tasks_outcome_failed", "tasks_outcome_skipped", "tasks_stage_catalog", "tasks_stage_generation", "tasks_reason_cancelled", "tasks_reason_timeout", "tasks_reason_session_limit", "tasks_reason_usage_limit", "tasks_reason_authentication_required", "tasks_reason_model_unavailable", "tasks_reason_cli_incompatible", "tasks_reason_output_limit", "tasks_reason_unavailable", "tasks_reason_cooldown", "tasks_reason_runtime_unavailable", "tasks_reason_invalid_response", "tasks_reason_provider_error", "tasks_reason_invalid_json", "tasks_reason_contract_rejected"];

export function TaskWorkbench(props:{transport:Transport;providers:ProviderRow[]}) {
 const t=useTranslate();
 const [open,setOpen]=useState(false);
 return <details className="more task-workbench" onToggle={e=>setOpen(e.currentTarget.open)}><summary>{t("tasks_title")}</summary>{open&&<TaskWorkbenchContent {...props}/>}</details>;
}
function TaskWorkbenchContent({transport,providers}:{transport:Transport;providers:ProviderRow[]}) {
 const times=useTimes();
 const t=useTranslate();
 const label=(key:string)=>t(TASK_MESSAGES.find(id=>id===key)??"tasks_reason_provider_error");
 const load=useCallback(async():Promise<LoadResult<{diagnostics:TaskDiagnostics;config:TaskProfiles}>>=>{
  const [diagnostics,config]=await Promise.all([transport.request("runtime.tasks.diagnostics",{}),transport.request("runtime.tasks.profiles",{})]);
  if(!diagnostics.ok)return diagnostics;if(!config.ok)return config;
  return {ok:true,data:{diagnostics:diagnostics.result as TaskDiagnostics,config:config.result as TaskProfiles}};
 },[transport]);
 const {state,reload}=useCard(load);
 const [draft,setDraft]=useState<TaskProfiles>();
 const save=useAction(useCallback(async(p:TaskProfiles):Promise<LoadResult<true>>=>{
  const answer=await transport.request("runtime.tasks.configure",{configuration_revision:p.configurationRevision,profiles:p.profiles});
  if(!answer.ok)return answer;
  if(!isRecord(answer.result)||answer.result.configurationRevision!==p.configurationRevision+1)return {ok:false,error:{code:"unexpected",message:"incompatible task profile save response"}};
  return {ok:true,data:true};
 },[transport]));
 useEffect(()=>{if(save.state.phase==="ready"){setDraft(undefined);reload();}},[save.state,reload]);
 return <>
 <CardFrame title={t("tasks_diagnostics")} state={state} onReload={reload}>{({diagnostics,config})=><>
  <p role="status">{t("tasks_capacity",{active:diagnostics.active,limit:diagnostics.limit,queued:diagnostics.queued})}</p>
  <p className="detail">{t("tasks_privacy")}</p>
  <ul className="services task-pauses">{diagnostics.providers.filter(p=>p.retryAfterMs>0).map(p=><li key={p.provider}><strong>{p.provider}</strong><span>{t("tasks_paused",{seconds:Math.ceil(p.retryAfterMs/1000)})}</span><span>{label(`tasks_reason_${p.failureReason||"provider_error"}`)}</span></li>)}</ul>
  {!diagnostics.attempts.length?<p>{t("tasks_empty")}</p>:<ol className="services task-attempts" tabIndex={0} aria-label={t("tasks_diagnostics")}>{[...diagnostics.attempts].reverse().map((a,i)=><li key={`${a.taskId}-${a.startedAt}-${i}`}>
   <strong>{t("tasks_attempt",{id:a.taskId})} · {a.provider} / {a.model}</strong>
   <span>{label(`tasks_profile_${a.profile}`)} · {label(`tasks_outcome_${a.outcome}`)} · {t("tasks_duration",{ms:a.durationMs})}</span>
   <span>{label(`tasks_stage_${a.stage}`)} · {a.startedAt?`${times.day(a.startedAt)} ${times.clock(a.startedAt)}`:""}</span>
   {a.reason&&<span>{label(`tasks_reason_${a.reason}`)}{a.cause?` · ${label(`tasks_reason_${a.cause}`)}`:""}{a.retryAfterMs>0?` · ${t("tasks_paused",{seconds:Math.ceil(a.retryAfterMs/1000)})}`:""}</span>}
  </li>)}</ol>}
  <h3>{t("tasks_priorities")}</h3><p className="detail">{t("tasks_priority_hint")}</p>
  {save.state.phase==="error"&&<Problem error={save.state.error}/>}
  {!draft?<>
   <dl>{(["lightweight","analysis"] as const).map(profile=><div key={profile}><dt>{label(`tasks_profile_${profile}`)}</dt><dd>{config.profiles[profile].map(c=>`${c.provider} / ${c.model}`).join(" → ")||t("tasks_automatic")}</dd></div>)}</dl>
   <Button onClick={()=>setDraft(structuredClone(config))}>{t("setting_edit")}</Button></>:<form onSubmit={e=>{e.preventDefault();save.start(draft);}}>
   {(["lightweight","analysis"] as const).map(profile=><TaskPriority key={profile} profile={profile} rows={draft.profiles[profile]} providers={providers} busy={save.state.phase==="loading"} onChange={rows=>setDraft({...draft,profiles:{...draft.profiles,[profile]:rows}})}/>)}
   <Button type="submit" disabled={save.state.phase==="loading"}>{t("tasks_save")}</Button>
   <Button disabled={save.state.phase==="loading"} onClick={()=>setDraft(undefined)}>{t("runtime_confirm_cancel")}</Button>
  </form>}
 </>}</CardFrame></>;
}
function TaskPriority({profile,rows,providers,busy,onChange}:{profile:string;rows:TaskChoice[];providers:ProviderRow[];busy:boolean;onChange:(rows:TaskChoice[])=>void}) {
 const t=useTranslate();
 const label=(key:string)=>t(TASK_MESSAGES.find(id=>id===key)??"tasks_reason_provider_error");const id=useId();const [choice,setChoice]=useState("");
 const available=providers.filter(p=>p.enabled!==false).flatMap(p=>(p.declaredModels??[]).map(model=>({provider:p.id,model}))).filter(c=>!rows.some(r=>r.provider===c.provider&&r.model===c.model));
 return <fieldset disabled={busy}><legend>{label(`tasks_profile_${profile}`)}</legend>
 <ol className="list-editor-items">{rows.map((r,i)=><li key={`${r.provider}/${r.model}`}><span>{r.provider} / {r.model}</span>
 <Button disabled={i===0} aria-label={t("tasks_move_named",{model:r.model})} onClick={()=>{const copy=[...rows];[copy[i-1],copy[i]]=[copy[i]!,copy[i-1]!];onChange(copy);}}>{t("tasks_move_up")}</Button>
 <Button aria-label={t("list_remove_named",{name:r.model})} onClick={()=>onChange(rows.filter((_,index)=>index!==i))}>{t("list_remove")}</Button></li>)}</ol>
 <label htmlFor={id}>{t("provider_choose_model")}</label><select id={id} value={choice} onChange={e=>setChoice(e.target.value)}><option value="">{t("provider_choose_model")}</option>{available.map(c=><option key={`${c.provider}|${c.model}`} value={`${c.provider}|${c.model}`}>{c.provider} / {c.model}</option>)}</select>
 <Button disabled={!choice||rows.length>=16} onClick={()=>{const selected=available.find(c=>`${c.provider}|${c.model}`===choice);if(selected)onChange([...rows,selected]);setChoice("");}}>{t("list_add")}</Button>
 </fieldset>;
}
