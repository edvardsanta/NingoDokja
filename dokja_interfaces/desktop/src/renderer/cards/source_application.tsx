import { useCallback, useEffect, useRef, useState } from "react";
import type { ActionType } from "../../shared/actions.js";
import type { KnowledgeDocument, KnowledgeDocuments } from "../../shared/replies.js";
import { sourceActive, sourceApplication, SOURCE_CONTRACT, type SourceApplication, type SourceAvailability, type SourcePreview, type SourceSnapshot } from "../../shared/source_application.js";
import { isRecord } from "../../shared/records.js";
import type { Transport, TransportError } from "../../shared/transport.js";
import { useTimes, useTranslate } from "../i18n/context.js";
import type { MessageId } from "../i18n/i18n.js";
import { Problem } from "./card_frame.js";
import { Button, Notice } from "./ui.js";
import { useWorkspaceRefresh } from "./workspace_panel.js";

const STATE_LABELS = {created:"source_state_created",reassessing:"source_state_reassessing",running:"source_state_running",paused:"source_state_paused",prepared:"source_state_prepared",failed:"source_state_failed",cancelled:"source_state_cancelled",expired:"source_state_expired",exhausted:"source_state_exhausted"} as const;
const CONTROL_LABELS:Record<string,MessageId> = {pause:"source_control_pause",resume:"source_control_resume",cancel:"source_control_cancel",redirect:"source_control_redirect"};
const RECEIPT_LABELS = {succeeded:"source_receipt_succeeded",failed:"source_receipt_failed",unknown:"source_receipt_unknown",partial:"source_receipt_partial"} as const;
const terminal = (s: SourceSnapshot) => !sourceActive(s);
const snapshotReply = (v: unknown): v is SourceSnapshot => isRecord(v) && typeof v.applicationRef==="string" && typeof v.sequence==="number" && isRecord(v.application);
type Pending = {type: "knowledge.application.supervision.start" | "knowledge.application.supervision.control"; payload: Record<string,unknown>};

export function SourceApplicationView({ transport, active=true }: {transport: Transport; active?: boolean}) {
 const t=useTranslate(),{clock}=useTimes();
 const request=useCallback(async(type:ActionType,payload:Record<string,unknown>,options?:{timeoutMs?:number})=>{
  try{return await transport.request(type,payload,options);}catch{return {ok:false as const,error:{code:"unavailable" as const,message:"source request unavailable"}};}
 },[transport]);
 const [documents,setDocuments]=useState<KnowledgeDocument[]>([]),[offset,setOffset]=useState(0),[total,setTotal]=useState(0);
 const [goal,setGoal]=useState(""),[constraints,setConstraints]=useState(""),[selected,setSelected]=useState<string[]>([]),[reason,setReason]=useState("");
 const [preview,setPreview]=useState<SourceApplication>(),[snapshot,setSnapshot]=useState<SourceSnapshot>(),[availability,setAvailability]=useState<SourceAvailability>();
 const [checking,setChecking]=useState(false);
 const checkingRef=useRef(false);
 const [busy,setBusy]=useState(false),[error,setError]=useState<TransportError>(),[rejected,setRejected]=useState(false),[editing,setEditing]=useState(false),[pending,setPending]=useState<Pending>();
 const progress=useRef<HTMLDivElement>(null),goalField=useRef<HTMLTextAreaElement>(null),reviewAction=useRef<HTMLButtonElement>(null);
 const [focusRequest,askFocus]=useState(0);
 useEffect(()=>{if(focusRequest&&active)(progress.current??goalField.current)?.focus();},[focusRequest]);
 useEffect(()=>{if(editing&&active)goalField.current?.focus();},[editing]);
 useEffect(()=>{if(preview&&active)reviewAction.current?.focus();},[preview]);
 const mounted=useRef(true),latest=useRef<SourceSnapshot|undefined>(undefined),pendingRef=useRef<Pending|undefined>(undefined),activeRef=useRef<string|undefined>(undefined),hostRef=useRef<string|undefined>(undefined);
 const polling=useRef(false),submitting=useRef(false);
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false;};},[]);
 const accept=useCallback((next:SourceSnapshot)=>{
  if(!mounted.current || (activeRef.current && activeRef.current!==next.applicationRef))return;
  if(latest.current && next.sequence<latest.current.sequence)return;
  activeRef.current=next.applicationRef;latest.current=next;setSnapshot(next);
  const op=pendingRef.current?.payload.operation_ref;
  if(op && next.controls.some((c)=>c.operationRef===op)){pendingRef.current=undefined;setPending(undefined);}
 },[]);
 const read=useCallback(async(ref:string)=>{
  const answer=await request("knowledge.application.supervision.status",{application_ref:ref});
  if(!mounted.current || activeRef.current!==ref)return;
  if(answer.ok && snapshotReply(answer.result) && answer.result.applicationRef===ref){if(latest.current&&answer.result.sequence<latest.current.sequence)return;accept(answer.result);setError(undefined);}
  else setError(answer.ok?{code:"unexpected",message:"invalid source snapshot"}:answer.error);
 },[request,accept]);
 const capabilities=useCallback(async(restore=false)=>{
  const answer=await request("knowledge.application.supervision.status",{});
  if(!mounted.current)return;
  if(answer.ok && isRecord(answer.result) && answer.result.available===true && typeof answer.result.hostRef==="string"){
   const info=answer.result as SourceAvailability;setAvailability(info);
   const retry=pendingRef.current;
   const known=info.applications?.find((a)=>a.operationRef===retry?.payload.operation_ref);
   const candidate=known ?? (restore && !activeRef.current ? info.applications?.[0]:undefined);
   if(candidate){activeRef.current=candidate.applicationRef;hostRef.current=info.hostRef;pendingRef.current=undefined;setPending(undefined);await read(candidate.applicationRef);}
  }else setError(answer.ok?{code:"unexpected",message:"invalid source availability"}:answer.error);
 },[request,read]);
 const loadDocuments=useCallback(async(page:number)=>{
  const answer=await request("knowledge.list",{limit:20,offset:page});
  if(!mounted.current)return;
  if(answer.ok && isRecord(answer.result) && Array.isArray(answer.result.documents)) {const list=answer.result as KnowledgeDocuments;setDocuments(list.documents);setTotal(list.total);setOffset(page);}
  else if(!answer.ok)setError(answer.error);
 },[request]);
 const refresh=useCallback(()=>{void capabilities();if(activeRef.current)void read(activeRef.current);},[capabilities,read]);
 useWorkspaceRefresh(refresh,busy);
 useEffect(()=>{void Promise.all([capabilities(true),loadDocuments(0)]);},[capabilities,loadDocuments]);
 useEffect(()=>{
  if(!active || !snapshot || (terminal(snapshot)&&snapshot.pending===0))return;
  const timer=setInterval(()=>{if(!polling.current && activeRef.current && document.visibilityState!=="hidden"){
   polling.current=true;void read(activeRef.current).finally(()=>{polling.current=false;});
  }},1000);return()=>clearInterval(timer);
 },[active,snapshot?.applicationRef,snapshot?.state,snapshot?.pending,read]);
 const candidate=sourceApplication({contract:SOURCE_CONTRACT,goal,constraints:constraints.split("\n").map((v)=>v.trim()).filter(Boolean),sources:selected.map((source_id)=>({source_id}))},false);
 const invalidate=()=>{setPreview(undefined);setRejected(false);};
 const check=async()=>{
  if(!candidate || submitting.current)return;submitting.current=true;checkingRef.current=true;setChecking(true);setBusy(true);setError(undefined);setRejected(false);
  try{const current=latest.current;const answer=await request(current?"knowledge.application.supervision.preview":"knowledge.application.prepare",current?{application_ref:current.applicationRef,expected_revision:current.controlRevision,application:candidate}:{...candidate},{timeoutMs:20000});if(!mounted.current || (current && latest.current?.controlRevision!==current.controlRevision))return;
   if(!answer.ok)setError(answer.error);else {const checked=answer.result as SourcePreview;if(checked.status==="prepared" && checked.application)setPreview(checked.application);else setRejected(true);}
  }finally{submitting.current=false;checkingRef.current=false;if(mounted.current){setBusy(false);setChecking(false);}}
 };
 const send=async(command:Pending)=>{
  if(submitting.current && !(checkingRef.current && command.payload.action==="cancel"))return;submitting.current=true;setBusy(true);setError(undefined);pendingRef.current=command;setPending(command);
 if(command.type==="knowledge.application.supervision.start")hostRef.current=String(command.payload.host_ref);
  try{const answer=await request(command.type,command.payload);
   if(!mounted.current)return;
   if(!answer.ok){setError(answer.error);if(["invalid","denied","orchestrator"].includes(answer.error.code)){pendingRef.current=undefined;setPending(undefined);if(activeRef.current)void read(activeRef.current);}return;}
   if(!snapshotReply(answer.result) || (command.type==="knowledge.application.supervision.control" && (answer.result.applicationRef!==command.payload.application_ref || !answer.result.controls.some((c)=>c.operationRef===command.payload.operation_ref)))){setError({code:"unexpected",message:"invalid control reply"});return;}
   if(command.type==="knowledge.application.supervision.start") {activeRef.current=answer.result.applicationRef;latest.current=undefined;hostRef.current=String(command.payload.host_ref);}
   pendingRef.current=undefined;setPending(undefined);setError(undefined);accept(answer.result);setEditing(false);setPreview(undefined);askFocus((n)=>n+1);
  }finally{submitting.current=false;if(mounted.current)setBusy(false);}
 };
 const control=(action:string,application?:SourceApplication)=>{
  const current=latest.current;if(!current)return;
  void send({type:"knowledge.application.supervision.control",payload:{application_ref:current.applicationRef,operation_ref:crypto.randomUUID(),expected_revision:current.controlRevision,action,reason:action==="redirect"?reason:"",...(application?{application}: {})}});
 };
 const apply=()=>{
  if(!preview)return;
  if(snapshot && sourceActive(snapshot)){control("redirect",preview);return;}
  if(availability)void send({type:"knowledge.application.supervision.start",payload:{operation_ref:crypto.randomUUID(),host_ref:availability.hostRef,application:preview}});
 };
 const edit=()=>{if(!snapshot)return;setGoal(snapshot.application.goal);setConstraints(snapshot.application.constraints.join("\n"));setSelected(snapshot.application.sources.map((s)=>s.source_id));setReason("");invalidate();setEditing(true);};
 const newPreparation=()=>{activeRef.current=undefined;latest.current=undefined;hostRef.current=undefined;pendingRef.current=undefined;setPending(undefined);setSnapshot(undefined);setPreview(undefined);setEditing(false);setError(undefined);askFocus((n)=>n+1);void capabilities();};
 const lost=!!hostRef.current && !!availability && hostRef.current!==availability.hostRef;
 const title=(id:string)=>documents.find((d)=>d.id===id)?.title || preview?.sources.find((s)=>s.source_id===id)?.title || id;
 const stateKey=snapshot ? STATE_LABELS[snapshot.state]:undefined;
 const validated=snapshot?.receipts.filter((r)=>r.controlRevision===snapshot.controlRevision && r.planRevision===snapshot.planRevision && r.confirmed && r.outcome==="succeeded").length ?? 0;
 return <section className="source-application" aria-labelledby="source-application-title">
  <header><h2 id="source-application-title">{t("source_title")}</h2><p className="detail">{t("source_intro")}</p></header>
  {error && <><Problem error={error}/><Notice boxed tone="unchecked">{t("source_connection_hint")}</Notice></>}
  {!availability && <p className="detail">{t("source_setup_hint")}</p>}
  {lost && <Notice boxed tone="problem" announce="polite">{t("source_lost")}</Notice>}
  <div className="source-toolbar"><Button size="compact" onClick={refresh} disabled={busy}>{t("refresh")}</Button>
   {pending && !lost && <Button onClick={()=>void send(pending)} disabled={busy}>{t("source_retry")}</Button>}
   {(lost || (snapshot&&terminal(snapshot)&&!pending))&& <Button onClick={newPreparation} disabled={busy}>{t("source_new")}</Button>}
  </div>
  {snapshot && <div className="source-progress" ref={progress} tabIndex={-1} role="region" aria-label={t("source_progress")}>
   <Notice announce="polite" boxed tone={snapshot.uncertain?"problem":snapshot.state==="prepared"?"ok":"progress"}>{t(stateKey!)} · {t("source_plan",{plan:snapshot.planRevision})}</Notice>
   <p>{snapshot.application.goal}</p>
   <progress max={snapshot.application.sources.length} value={Math.min(validated,snapshot.application.sources.length)} aria-label={t("source_progress")}/>
   <p className="detail">{t("source_counts",{done:validated,total:snapshot.application.sources.length,attempts:snapshot.readAttempts})} · {t("source_deadline",{time:clock(snapshot.deadline)})}</p>
   {snapshot.pending>0 && <p className="detail">{t("source_pending")}</p>}
   {snapshot.uncertain && <Notice boxed tone="problem">{t("source_uncertain")}</Notice>}
   {sourceActive(snapshot)&&!lost && <><p className="detail">{t("source_pause_hint")}</p><div className="source-toolbar">
    {snapshot.state!=="paused" && <Button disabled={busy||!!pending} onClick={()=>control("pause")}>{t("source_pause")}</Button>}
    {snapshot.state==="paused" && <Button disabled={busy||!!pending||snapshot.pending>0||snapshot.uncertain} onClick={()=>control("resume")}>{t("source_resume")}</Button>}
    <Button onClick={edit} disabled={busy||!!pending||snapshot.state!=="paused"||snapshot.pending>0}>{t("source_adjust")}</Button>
    <Button disabled={busy&&!checking||!!pending} onClick={()=>control("cancel")}>{t("source_cancel")}</Button>
   </div></>}
   {snapshot.receipts.length>0 && <section aria-label={t("source_receipts")}><h3>{t("source_receipts")}</h3><ul className="source-receipts">{snapshot.receipts.map((r)=><li key={r.attemptRef} data-tone={!r.confirmed||r.outcome==="unknown"?"unchecked":r.outcome==="failed"?"problem":"ok"}>
    <strong>{title(r.sourceId)}</strong><span>{t((!r.confirmed?"source_receipt_unconfirmed":RECEIPT_LABELS[r.outcome]) as MessageId)} · {t("source_plan",{plan:r.planRevision})}</span>
   </li>)}</ul></section>}
   {snapshot.controls.length>0 && <details><summary>{t("source_feedback")}</summary><ul>{snapshot.controls.map((c)=><li key={c.operationRef}>{t(CONTROL_LABELS[c.action]!)} · {t("source_plan",{plan:c.planRevision})}{c.reason && <p>{c.reason}</p>}</li>)}</ul></details>}
  </div>}
  {((!snapshot&&!activeRef.current)||editing)&&!lost && <form className="source-form" onSubmit={(e)=>{e.preventDefault();void check();}}>
   <fieldset disabled={busy||!!pending}><legend>{t(snapshot?"source_correction":"source_goal_section")}</legend>
    <label>{t("source_goal")}<textarea ref={goalField} value={goal} maxLength={2000} rows={3} onChange={(e)=>{setGoal(e.target.value);invalidate();}} required/></label>
    <label>{t("source_constraints")}<textarea value={constraints} rows={2} onChange={(e)=>{setConstraints(e.target.value);invalidate();}}/><span className="detail">{t("source_constraints_hint")}</span></label>
    {snapshot && <label>{t("source_reason")}<textarea value={reason} maxLength={1000} rows={3} onChange={(e)=>setReason(e.target.value)} required/></label>}
    <fieldset className="source-picker"><legend>{t("source_documents",{count:selected.length})}</legend>
     {documents.map((d)=><label key={d.id}><input type="checkbox" checked={selected.includes(d.id)} disabled={!selected.includes(d.id)&&selected.length>=8} onChange={(e)=>{setSelected((ids)=>e.target.checked?[...ids,d.id]:ids.filter((id)=>id!==d.id));invalidate();}}/><span>{d.title||d.id}<small className="detail">{d.kind}</small></span></label>)}
     {!documents.length && <p>{t("source_empty")}</p>}
     <div className="source-toolbar"><Button disabled={offset===0} onClick={()=>void loadDocuments(Math.max(0,offset-20))}>{t("source_previous")}</Button><Button disabled={offset+documents.length>=total} onClick={()=>void loadDocuments(offset+20)}>{t("source_next")}</Button></div>
     {selected.some((id)=>!documents.some((d)=>d.id===id)) && <p className="detail">{t("source_selected_elsewhere")}</p>}
    </fieldset>
    {!candidate&&goal.trim()&&selected.length>0&&<p role="status" className="detail">{t("source_invalid")}</p>}
    <Button type="submit" disabled={!candidate || !!snapshot&&!reason.trim()}>{t(busy?"source_checking":"source_check")}</Button>
   </fieldset>
   {rejected && <Notice boxed tone="problem" announce="polite">{t("source_rejected")}</Notice>}
   {preview && <div className="source-reviewed"><Notice boxed tone="ok" announce="polite">{t("source_checked",{count:preview.sources.length})}</Notice><ul>{preview.sources.map((s)=><li key={s.source_id}>{s.title||title(s.source_id)}</li>)}</ul>
    <Button ref={reviewAction} variant="primary" disabled={busy||!!pending||!availability||availability.remaining===0&&!snapshot || !!snapshot&&!reason.trim()} onClick={apply}>{t(snapshot?"source_apply_correction":"source_start")}</Button>
   </div>}
  </form>}
  <p className="detail source-session-note">{t("source_session_hint")}</p>
 </section>;
}
