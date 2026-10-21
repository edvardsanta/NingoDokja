import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { MemoryExperiencesPanel } from "../../src/renderer/cards/memory_experiences.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { MemoryExperience } from "../../src/shared/replies.js";
import { deferred, failure, ok, routedTransport } from "./support.js";

const entry:MemoryExperience={ref:"fixture:1",action:"hashtag.suggest",detail:"#Reading",outcome:"",predictedP:0,baselineP:0.5,matchedScore:0.9,contextSnippet:"A context excerpt",matchedSnippet:"An earlier example",contextTruncated:false,createdAt:"2026-10-21T00:00:00Z",resolvedAt:"",feedback:{code:"hashtag_tag",message:""}};
const page=(entries=[entry],total=7,offset=0)=>ok({experiences:entries,total,offset,limit:20});
const detail=(extra:Partial<MemoryExperience>={})=>ok({found:true,experience:{...entry,context:"<b>Full</b> context\nNext line",...extra}});
const show=(transport:Parameters<typeof MemoryExperiencesPanel>[0]["transport"])=>render(<I18nProvider locale="en"><MemoryExperiencesPanel transport={transport}/></I18nProvider>);

describe("saved experiences",()=>{
 it("lists the seven experiences, opens context as text and explains feedback without writing",async()=>{
  const seven=Array.from({length:7},(_,i)=>({...entry,ref:`fixture:${i+1}`}));
  const {transport,calls}=routedTransport({"memory.list":page(seven),"memory.get":detail()});
  const view=show(transport);
  expect(calls).toHaveLength(0);
  fireEvent.click(screen.getByRole("button",{name:"Load experiences"}));
  await screen.findByText("7 experiences match this filter.");
  expect(screen.getAllByRole("button",{name:/Inspect fixture:/})).toHaveLength(7);
  fireEvent.click(screen.getByRole("button",{name:"Inspect fixture:1"}));
  const context=await screen.findByText(/<b>Full<\/b> context/);
  expect(context.tagName).toBe("PRE");
  expect(view.container.querySelector("pre b")).toBeNull();
  screen.getByText("0.0%");screen.getByText("50.0%");screen.getByText("90.0%");
  screen.getByText("No observed outcome yet.");
  screen.getByText(/Keeping the suggested hashtag means accepted/);
  expect(calls.map(c=>[c.type,c.payload])).toEqual([["memory.list",{state:"all",limit:20,offset:0}],["memory.get",{ref:"fixture:1"}]]);
 });
 it("pages and filters without losing the selected filter",async()=>{
  const first=Array.from({length:20},(_,i)=>({...entry,ref:`fixture:${i}`}));
  const {transport,calls}=routedTransport({"memory.list":[page(first,21),page([{...entry,ref:"fixture:20"}],21,20),page([],0)]});
  show(transport);
  fireEvent.click(screen.getByRole("button",{name:"Load experiences"}));
  await screen.findByText("21 experiences match this filter.");
  fireEvent.click(screen.getByRole("button",{name:"Next experiences"}));
  await screen.findByRole("button",{name:"Inspect fixture:20"});
  fireEvent.change(screen.getByLabelText("Show"),{target:{value:"resolved"}});
  await screen.findByText("No experiences match this filter.");
  expect(calls.map(c=>c.payload)).toEqual([{state:"all",limit:20,offset:0},{state:"all",limit:20,offset:20},{state:"resolved",limit:20,offset:0}]);
 });
 it("distinguishes missing predictions from zero and observed outcomes from pending",async()=>{
  const {transport}=routedTransport({"memory.list":page(),"memory.get":detail({predictedP:null,baselineP:null,outcome:"accepted",resolvedAt:"2026-10-22T00:00:00Z",feedback:{code:"recorded",message:""}})});
  show(transport);fireEvent.click(screen.getByRole("button",{name:"Load experiences"}));
  fireEvent.click(await screen.findByRole("button",{name:"Inspect fixture:1"}));
  await screen.findByText("The outcome is already recorded. The first verdict is retained.");
  expect(screen.getAllByText("Not recorded")).toHaveLength(2);
  screen.getByText("accepted");screen.getByText("2026-10-22T00:00:00Z");
 });
 it("handles errors and a deleted record, and drops a late detail after closing",async()=>{
  const delayed=deferred<ReturnType<typeof ok>>();
  const {transport,calls}=routedTransport({"memory.list":[failure("timeout"),page()],"memory.get":[ok({found:false}),()=>delayed.promise]});
  show(transport);fireEvent.click(screen.getByRole("button",{name:"Load experiences"}));
  await screen.findByRole("alert");fireEvent.click(screen.getByRole("button",{name:"Load experiences"}));
  fireEvent.click(await screen.findByRole("button",{name:"Inspect fixture:1"}));
  await screen.findByText("This experience is no longer available. Reload the list.");
  fireEvent.click(screen.getByRole("button",{name:"Inspect fixture:1"}));
  fireEvent.click(screen.getByRole("button",{name:"Inspect fixture:1"}));
  await waitFor(()=>expect(calls.filter(c=>c.type==="memory.get")).toHaveLength(2));
  fireEvent.click(screen.getByRole("button",{name:"Inspect fixture:1"}));
  delayed.resolve(detail());
  expect(screen.queryByText(/<b>Full/)).toBeNull();
 });
});

describe("explicit experience feedback",()=>{
 async function open(routes:Parameters<typeof routedTransport>[0]) {
  const test=routedTransport({"memory.list":page(),"memory.get":detail(),...routes});
  show(test.transport);fireEvent.click(screen.getByRole("button",{name:"Load experiences"}));
  fireEvent.click(await screen.findByRole("button",{name:"Inspect fixture:1"}));
  await screen.findByRole("button",{name:"Accept result"});return test;
 }
 it("sends acceptance for the inspected ref only after the operator clicks",async()=>{
  const {calls}=await open({"memory.resolve":ok({found:true,resolved:true,outcome:"accepted"})});
  expect(calls.some(c=>c.type==="memory.resolve")).toBe(false);
  fireEvent.click(screen.getByRole("button",{name:"Accept result"}));
  await screen.findByText("Feedback saved: accepted.");
  expect(calls.at(-1)?.payload).toEqual({ref:"fixture:1",outcome:"accepted"});
  expect(screen.queryByRole("button",{name:"Accept result"})).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"Refresh memory"}));
  await waitFor(()=>expect(calls.filter(c=>c.type==="memory.list")).toHaveLength(2));
 });
 it("submits an actual result without deciding acceptance or replacement in the interface",async()=>{
  const {calls}=await open({"memory.resolve":ok({found:true,resolved:true,outcome:"replaced"})});
  expect((screen.getByRole("button",{name:"Save actual result"}) as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText("Actual result"),{target:{value:" #Other "}});
  fireEvent.click(screen.getByRole("button",{name:"Save actual result"}));
  await screen.findByText("Feedback saved: replaced.");
  expect(calls.at(-1)?.payload).toEqual({ref:"fixture:1",observed:"#Other"});
 });
 it("reports a concurrent verdict without claiming the new choice was saved",async()=>{
  const {calls}=await open({"memory.resolve":ok({found:true,resolved:false,outcome:"accepted"})});
  fireEvent.click(screen.getByRole("button",{name:"Not used"}));
  await screen.findByText("An outcome was already recorded: accepted. Your submission did not replace it.");
  expect(calls.at(-1)?.payload).toEqual({ref:"fixture:1",outcome:"ignored"});
 });
 it("leaves outcomes unknown on timeout and does not retry automatically",async()=>{
  const {calls}=await open({"memory.resolve":failure("timeout")});
  fireEvent.click(screen.getByRole("button",{name:"Accept result"}));
  await screen.findByText(/Refresh memory before trying again/);
  expect(calls.filter(c=>c.type==="memory.resolve")).toHaveLength(1);
 });
 it("shows a disabled service without claiming a saved result",async()=>{
  await open({"memory.resolve":ok({off:true,reason:"paused"})});
  fireEvent.click(screen.getByRole("button",{name:"Accept result"}));
  await screen.findByText(/paused/);
  expect(screen.queryByText(/Feedback saved:/)).toBeNull();
 });
 it("hides controls for a recorded verdict",async()=>{
  const test=routedTransport({"memory.list":page(),"memory.get":detail({outcome:"accepted",resolvedAt:"2026-10-21T01:00:00Z"})});
  show(test.transport);fireEvent.click(screen.getByRole("button",{name:"Load experiences"}));
  fireEvent.click(await screen.findByRole("button",{name:"Inspect fixture:1"}));
  await screen.findByText("2026-10-21T01:00:00Z");
  expect(screen.queryByRole("button",{name:"Accept result"})).toBeNull();
 });
});
