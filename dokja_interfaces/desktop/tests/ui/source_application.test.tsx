import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SourceApplicationView } from "../../src/renderer/cards/source_application.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import type { SourceApplication, SourceSnapshot } from "../../src/shared/source_application.js";
import type { ActionType } from "../../src/shared/actions.js";
import { deferred } from "./support.js";
function fixture(){
 const calls:{type:ActionType;payload:Record<string,unknown>}[]=[];let current:SourceSnapshot|undefined;let previews=0,lose=false;let nextRead:Promise<TransportResult>|undefined;let nextPreview:Promise<TransportResult>|undefined;const seen=new Set<string>();
 const ok=(result:unknown):TransportResult=>({ok:true,result:structuredClone(result)});
 const transport:Transport={preview:async()=>({ok:false,error:{code:"denied",message:"unused"}}),request:async(type,payload={})=>{
 calls.push({type,payload:structuredClone(payload)});
 if(type==="knowledge.list")return ok({total:2,offset:0,documents:[{id:"guide",title:"Retry guide",kind:"note"},{id:"api",title:"API contract",kind:"note"}]});
 if(type==="knowledge.application.prepare"||type==="knowledge.application.supervision.preview"){if(nextPreview){const promise=nextPreview;nextPreview=undefined;return promise;}previews++;const app=structuredClone(type==="knowledge.application.supervision.preview"?payload.application:payload) as unknown as SourceApplication;app.sources=app.sources.map((s)=>({...s,text_version:"sha256:"+String(previews).repeat(64),title:s.source_id==="guide"?"Retry guide":"API contract"}));return ok({status:"prepared",application:app});}
 if(type==="knowledge.application.supervision.status"){
  if(!payload.application_ref)return ok({available:true,hostRef:"host:one",remaining:16,applications:[]});
  if(nextRead){const read=nextRead;nextRead=undefined;return read;}return ok(current);
 }
 if(type==="knowledge.application.supervision.start"){current={applicationRef:"application:one",state:"running",reason:"",sequence:3,controlRevision:1,planRevision:1,readAttempts:1,pending:0,uncertain:false,deadline:"2030-01-01T00:00:00Z",application:structuredClone(payload.application) as SourceApplication,controls:[],receipts:[{attemptRef:"attempt:old",sourceId:"guide",controlRevision:1,planRevision:1,outcome:"succeeded",confirmed:true}]};return ok(current);}
 if(type==="knowledge.application.supervision.control"&&current){const op=String(payload.operation_ref);if(!seen.has(op)){seen.add(op);current.controlRevision++;current.sequence+=10;
  if(payload.action==="pause")current.state="paused";if(payload.action==="resume")current.state="running";if(payload.action==="cancel")current.state="cancelled";
  if(payload.action==="redirect"){current.planRevision++;current.application=structuredClone(payload.application) as SourceApplication;}
  current.controls.push({operationRef:op,actor:"desktop:operator",action:String(payload.action),reason:String(payload.reason),controlRevision:current.controlRevision,planRevision:current.planRevision});
 }if(lose){lose=false;return {ok:false,error:{code:"timeout",message:"lost acknowledgment"}};}return ok(current);}
 return {ok:false,error:{code:"unexpected",message:"unhandled fixture"}};
 }};
 return {transport,calls,current:()=>structuredClone(current),loseNext:()=>{lose=true;},deferRead:(v:Promise<TransportResult>)=>{nextRead=v;},deferPreview:(v:Promise<TransportResult>)=>{nextPreview=v;}};
}
function mount(transport:Transport){return render(<I18nProvider locale="en"><SourceApplicationView transport={transport}/></I18nProvider>);}
async function start(f:ReturnType<typeof fixture>){mount(f.transport);await screen.findByRole("checkbox",{name:/Retry guide/});
 fireEvent.change(screen.getByLabelText("What should these documents help with?"),{target:{value:"Apply the retry policy"}});
 fireEvent.click(screen.getByRole("checkbox",{name:/Retry guide/}));fireEvent.click(screen.getByRole("checkbox",{name:/API contract/}));
 fireEvent.click(screen.getByRole("button",{name:"Check selected versions"}));await screen.findByText(/2 document versions checked/);
 fireEvent.click(screen.getByRole("button",{name:"Start supervised preparation"}));await screen.findByRole("button",{name:"Pause"});
}
describe("Library source supervision",()=>{
 it("checks versions and sends pinned sources without screen-provided authority",async()=>{const f=fixture();await start(f);
  expect(f.calls.find((c)=>c.type==="knowledge.application.prepare")!.payload.sources).toEqual([{source_id:"guide"},{source_id:"api"}]);
  const command=f.calls.find((c)=>c.type==="knowledge.application.supervision.start")!;expect(command.payload.host_ref).toBe("host:one");expect(command.payload.operation_ref).toMatch(/^[a-f0-9-]{36}$/);
  expect(JSON.stringify(command.payload)).not.toMatch(/token|is_owner/);expect((command.payload.application as SourceApplication).sources[0]!.text_version).toBe("sha256:"+"1".repeat(64));
  expect(screen.getByRole("progressbar").getAttribute("value")).toBe("1");expect(screen.getByText(/does not prove that a method was understood/)).toBeTruthy();
 });
 it("pauses, corrects with fresh pins and reasons, resumes and cancels",async()=>{const f=fixture();await start(f);
  fireEvent.click(screen.getByRole("button",{name:"Pause"}));await screen.findByRole("button",{name:"Resume"});fireEvent.click(screen.getByRole("button",{name:"Correct direction"}));
  fireEvent.change(screen.getByLabelText("What should these documents help with?"),{target:{value:"Use API version one"}});fireEvent.change(screen.getByLabelText("Why is the current approach wrong?"),{target:{value:"The plan assumes version two"}});
  fireEvent.click(screen.getByRole("button",{name:"Check selected versions"}));await screen.findByText(/2 document versions checked/);fireEvent.click(screen.getByRole("button",{name:"Apply correction"}));await screen.findByText("Use API version one");
  const command=f.calls.find((c)=>c.type==="knowledge.application.supervision.control"&&c.payload.action==="redirect")!;expect(command.payload.expected_revision).toBe(2);expect(command.payload.reason).toBe("The plan assumes version two");expect((command.payload.application as SourceApplication).sources[0]!.text_version).toBe("sha256:"+"2".repeat(64));
  expect(f.current()!.receipts[0]!.planRevision).toBe(1);expect(screen.getByRole("progressbar").getAttribute("value")).toBe("0");fireEvent.click(screen.getByRole("button",{name:"Resume"}));await screen.findByRole("button",{name:"Pause"});fireEvent.click(screen.getByRole("button",{name:"Cancel preparation"}));await screen.findByText(/Preparation cancelled/);
  expect(f.calls.filter((c)=>c.type==="knowledge.application.supervision.control").map((c)=>[c.payload.action,c.payload.expected_revision])).toEqual([["pause",1],["redirect",2],["resume",3],["cancel",4]]);
 });
 it("reuses the exact command after an unconfirmed control reply",async()=>{const f=fixture();await start(f);f.loseNext();fireEvent.click(screen.getByRole("button",{name:"Pause"}));await screen.findByRole("button",{name:"Retry same command"});fireEvent.click(screen.getByRole("button",{name:"Retry same command"}));await screen.findByRole("button",{name:"Resume"});const commands=f.calls.filter((c)=>c.type==="knowledge.application.supervision.control");expect(commands).toHaveLength(2);expect(commands[0]!.payload).toEqual(commands[1]!.payload);expect(f.current()!.controlRevision).toBe(2);});
 it("drops a stale poll after a control",async()=>{const f=fixture();await start(f);const old=f.current()!,late=deferred<TransportResult>();f.deferRead(late.promise);fireEvent.click(screen.getByRole("button",{name:"Refresh"}));await waitFor(()=>expect(f.calls.some((c)=>c.type==="knowledge.application.supervision.status"&&c.payload.application_ref)).toBe(true));fireEvent.click(screen.getByRole("button",{name:"Pause"}));await screen.findByRole("button",{name:"Resume"});late.resolve({ok:true,result:old});await waitFor(()=>expect(screen.queryByRole("button",{name:"Pause"})).toBeNull());expect(screen.getByRole("button",{name:"Resume"})).toBeTruthy();});
 it("can cancel during a candidate check and discard its late result",async()=>{
  const f=fixture();await start(f);fireEvent.click(screen.getByRole("button",{name:"Pause"}));await screen.findByRole("button",{name:"Resume"});fireEvent.click(screen.getByRole("button",{name:"Correct direction"}));
  fireEvent.change(screen.getByLabelText("Why is the current approach wrong?"),{target:{value:"Inspect the corrected candidate"}});
  const pending=deferred<TransportResult>();f.deferPreview(pending.promise);fireEvent.click(screen.getByRole("button",{name:"Check selected versions"}));
  await waitFor(()=>expect(f.calls.some(c=>c.type==="knowledge.application.supervision.preview")).toBe(true));
  const cancel=screen.getByRole("button",{name:"Cancel preparation"});expect((cancel as HTMLButtonElement).disabled).toBe(false);fireEvent.click(cancel);await screen.findByText(/Preparation cancelled/);
  pending.resolve({ok:true,result:{status:"prepared",application:f.current()!.application}});await waitFor(()=>expect(screen.queryByRole("button",{name:"Apply correction"})).toBeNull());
 });
 it("explains unavailable supervision while keeping document selection usable",async()=>{const f=fixture();const transport={...f.transport,request:async(type:ActionType,payload?:Record<string,unknown>)=>type.startsWith("knowledge.application.supervision.")?{ok:false as const,error:{code:"unavailable" as const,message:"not configured"}}:f.transport.request(type,payload)};mount(transport);await screen.findByText(/Supervised preparation needs/);expect(await screen.findByRole("checkbox",{name:/Retry guide/})).toBeTruthy();});
});
