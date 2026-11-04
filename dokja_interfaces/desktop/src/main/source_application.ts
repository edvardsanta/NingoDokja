import { isRecord, text } from "../shared/records.js";
import { sourceApplication, SOURCE_REF, SOURCE_STATES, type SourceAvailability, type SourcePreview, type SourceSnapshot } from "../shared/source_application.js";
const ref = (value: unknown): value is string => typeof value==="string" && SOURCE_REF.test(value);
const number = (v:unknown,max=10000):number => {if(typeof v!=="number" || !Number.isSafeInteger(v) || v<0 || v>max)throw new Error("invalid source supervision counter");return v;};
export function sourceStart(raw:Record<string,unknown>):Record<string,unknown>|undefined{
 const application=sourceApplication(raw.application,true);
 return application && ref(raw.operation_ref) && ref(raw.host_ref) ? {application,operation_ref:raw.operation_ref,host_ref:raw.host_ref}:undefined;
}
export function sourceStatus(raw:Record<string,unknown>):Record<string,unknown>|undefined{
 return Object.keys(raw).length===0 ? {} : ref(raw.application_ref) ? {application_ref:raw.application_ref}:undefined;
}
export function sourceControl(raw:Record<string,unknown>):Record<string,unknown>|undefined{
 if(!ref(raw.application_ref)||!ref(raw.operation_ref)||!Number.isSafeInteger(raw.expected_revision)||Number(raw.expected_revision)<1||Number(raw.expected_revision)>33||!["pause","resume","redirect","cancel"].includes(text(raw.action)))return undefined;
 const reason=text(raw.reason);if(Buffer.byteLength(reason)>1000)return undefined;
 const application=raw.action==="redirect" ? sourceApplication(raw.application,true):undefined;
 if(raw.action==="redirect" && (!application || !reason.trim()))return undefined;
 return {application_ref:raw.application_ref,operation_ref:raw.operation_ref,expected_revision:raw.expected_revision,action:raw.action,reason,...(application ? {application}: {})};
}
export function sourcePreview(body:Record<string,unknown>):SourcePreview{
 if(body.status!=="prepared")return {status:"not_prepared"};
 const application=sourceApplication(body.application,true);if(!application)throw new Error("invalid source preparation");
 const raw=isRecord(body.application)?body.application:{};
 if(Array.isArray(raw.sources))application.sources.forEach((s,i)=>{const item=raw.sources as unknown[];if(isRecord(item[i]))s.title=text(item[i].title).slice(0,300);});
 return {status:"prepared",application};
}
export function sourceSnapshot(body:Record<string,unknown>):SourceSnapshot|SourceAvailability{
 if(body.available===true && ref(body.host_ref)){
  if(!Array.isArray(body.applications)||body.applications.length>16||!body.applications.every((v)=>isRecord(v)&&ref(v.application_ref)&&ref(v.operation_ref)))throw new Error("invalid source application references");
  return {available:true,hostRef:body.host_ref,remaining:number(body.remaining,16),applications:body.applications.map((v)=>({applicationRef:String(v.application_ref),operationRef:String(v.operation_ref)}))};
 }
 const application=sourceApplication(body.application,true);
 if(!ref(body.application_ref)||!SOURCE_STATES.includes(body.state as never)||!application||typeof body.uncertain!=="boolean"||!Array.isArray(body.controls)||body.controls.length>32||!Array.isArray(body.receipts)||body.receipts.length>32||typeof body.deadline!=="string"||!Number.isFinite(Date.parse(body.deadline)))throw new Error("invalid source supervision snapshot");
 const controlRevision=number(body.control_revision,33),planRevision=number(body.plan_revision,33),pending=number(body.pending,32);
 if(!controlRevision||!planRevision || (body.state==="prepared" && (body.uncertain || pending)))throw new Error("inconsistent source preparation state");
 return {applicationRef:body.application_ref,state:body.state as SourceSnapshot["state"],reason:text(body.reason).slice(0,240),sequence:number(body.sequence),controlRevision,planRevision,pending,uncertain:body.uncertain,deadline:body.deadline,readAttempts:number(body.read_attempts,32),application,
  controls:body.controls.map((c)=>{if(!isRecord(c)||!ref(c.operation_ref)||!ref(c.actor)||!["pause","resume","redirect","cancel"].includes(text(c.action)))throw new Error("invalid source control record");return {operationRef:c.operation_ref,actor:c.actor,action:text(c.action),reason:text(c.reason).slice(0,1000),controlRevision:number(c.control_revision,33),planRevision:number(c.plan_revision,33)};}),
  receipts:body.receipts.map((r)=>{if(!isRecord(r)||!ref(r.attempt_ref)||!["succeeded","failed","unknown","partial"].includes(text(r.outcome))||typeof r.confirmed!=="boolean")throw new Error("invalid native source receipt");return {attemptRef:r.attempt_ref,sourceId:text(r.source_id).slice(0,240),controlRevision:number(r.state_revision,33),planRevision:number(r.plan_revision,33),outcome:r.outcome as SourceSnapshot["receipts"][number]["outcome"],confirmed:r.confirmed};}),
 };
}
