import { isRecord } from "./records.js";

export const SOURCE_STATES = ["created", "reassessing", "running", "paused", "prepared", "failed", "cancelled", "expired", "exhausted"] as const;
export type SourceState = typeof SOURCE_STATES[number];
export const SOURCE_CONTRACT = "knowledge.application.context.v1";
export type SourceSelection = { source_id: string; text_version?: string; title?: string };
export type SourceApplication = { contract: typeof SOURCE_CONTRACT; goal: string; constraints: string[]; sources: SourceSelection[] };
export type SourcePreview = { status: "prepared" | "not_prepared"; application?: SourceApplication };
export type SourceReceipt = { attemptRef: string; sourceId: string; controlRevision: number; planRevision: number; outcome: "succeeded" | "failed" | "unknown" | "partial"; confirmed: boolean };
export type SourceSnapshot = {
 applicationRef: string; state: SourceState; reason: string; sequence: number; controlRevision: number; planRevision: number;
 readAttempts: number; pending: number; uncertain: boolean; deadline: string; application: SourceApplication;
 controls: { operationRef: string; actor: string; action: string; reason: string; controlRevision: number; planRevision: number }[];
 receipts: SourceReceipt[];
};
export type SourceAvailability = { available: true; hostRef: string; remaining: number; applications: { applicationRef: string; operationRef: string }[] };
export const SOURCE_ID = /^[A-Za-z0-9._:/#@-]{1,240}$/;
export const SOURCE_REF = /^[A-Za-z0-9._:-]{1,240}$/;
const PIN = /^sha256:[a-f0-9]{64}$/;
const bytes = (text: string) => new TextEncoder().encode(text).length;
export function sourceApplication(raw: unknown, pinned: boolean): SourceApplication | undefined {
 if(isRecord(raw) && (raw.constraints===null || raw.constraints===undefined))raw={...raw,constraints:[]};
 if (!isRecord(raw) || raw.contract !== SOURCE_CONTRACT || typeof raw.goal !== "string" || !raw.goal.trim() || bytes(raw.goal)>2000 || !Array.isArray(raw.constraints) || raw.constraints.length>16 || !Array.isArray(raw.sources) || raw.sources.length<1 || raw.sources.length>8) return undefined;
 if (!raw.constraints.every((v): v is string => typeof v==="string" && !!v.trim() && bytes(v)<=500)) return undefined;
 const sources: SourceSelection[]=[];const seen=new Set<string>();
 for(const item of raw.sources){
  if (!isRecord(item) || typeof item.source_id!=="string" || !SOURCE_ID.test(item.source_id) || seen.has(item.source_id))return undefined;
  if (pinned && (typeof item.text_version!=="string" || !PIN.test(item.text_version)))return undefined;
  if (item.text_version!==undefined && (typeof item.text_version!=="string" || !PIN.test(item.text_version)))return undefined;
  seen.add(item.source_id);sources.push({source_id:item.source_id,...(typeof item.text_version==="string" ? {text_version:item.text_version}: {})});
 }
 const value: SourceApplication={contract:SOURCE_CONTRACT,goal:raw.goal,constraints:raw.constraints,sources};
 return bytes(JSON.stringify(value))<=8192 ? value : undefined;
}
export function sourceActive(snapshot: SourceSnapshot): boolean { return ["created","running","paused","reassessing"].includes(snapshot.state); }
