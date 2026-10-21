import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { RuntimeCard } from "../../src/renderer/cards/runtime.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { failure, ok, routedTransport } from "./support.js";
const page=(state="stopped",busy=false,error="")=>ok({modules:[{name:"memory",state,error,dependencies:["orchestrator","embedding_init"]},{name:"book",state:"stopped",error:"",dependencies:["orchestrator"]}],busy,error:""});
const show=(transport:Parameters<typeof RuntimeCard>[0]["transport"])=>render(<I18nProvider locale="en"><RuntimeCard transport={transport}/></I18nProvider>);
describe("local runtime modules",()=>{
 it("lists stopped modules without starting any container, even when the main orchestrator is down",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":page()});show(transport);
  await screen.findAllByText("Stopped");
  expect(calls.map(c=>c.type)).toEqual(["runtime.modules"]);
  expect((screen.getByRole("button",{name:"Stop memory"}) as HTMLButtonElement).disabled).toBe(true);
 });
 it("starts only the selected module and polls an accepted operation until ready",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":[page(),page("starting",true),page("ready")],"runtime.set":page("starting",true)});show(transport);
  fireEvent.click(await screen.findByRole("button",{name:"Start memory"}));
  await screen.findByText("Starting");
  expect((screen.getByRole("button",{name:"Start book"}) as HTMLButtonElement).disabled).toBe(true);
  await screen.findByText("Ready",{}, {timeout:3000});
  expect(calls.filter(c=>c.type==="runtime.set").map(c=>c.payload)).toEqual([{module:"memory",enabled:true}]);
  expect(calls.some(c=>c.type==="services.set")).toBe(false);
 });
 it("shows background readiness errors and permits an explicit retry",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":[page("failed",false,"Model initialization failed"),page("ready")],"runtime.set":page("starting",true)});show(transport);
  await screen.findByText("Model initialization failed");
  fireEvent.click(screen.getByRole("button",{name:"Start memory"}));
  await screen.findByText("Ready");expect(calls.filter(c=>c.type==="runtime.set")).toHaveLength(1);
 });
 it("refreshes after an uncertain write without automatically repeating it",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":page("ready"),"runtime.set":failure("timeout")});show(transport);
  fireEvent.click(await screen.findByRole("button",{name:"Stop memory"}));
  await screen.findByRole("alert");await waitFor(()=>expect(calls.filter(c=>c.type==="runtime.modules")).toHaveLength(2));
  expect(calls.filter(c=>c.type==="runtime.set")).toHaveLength(1);
 });
});

it("keeps container controls available when the main orchestrator is unavailable",async()=>{
 const {App}=await import("../../src/renderer/app.js");
 const {transport,calls}=routedTransport({"runtime.modules":page(),"ningo.status":failure("unavailable")});
 render(<App transport={transport} locale="en" runtimeControls/>);
 await screen.findByRole("heading",{name:"Local containers"});
 await screen.findByRole("button",{name:"Start memory"});
 expect(calls.some(c=>c.type==="ningo.status")).toBe(true);
 expect(calls.filter(c=>c.type==="runtime.set")).toHaveLength(0);
});
