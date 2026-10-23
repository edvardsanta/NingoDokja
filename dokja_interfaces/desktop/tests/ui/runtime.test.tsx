import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { RuntimeCard } from "../../src/renderer/cards/runtime.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { deferred, failure, ok, routedTransport, scriptedTransport } from "./support.js";
const page=(state="stopped",busy=false,error="")=>ok({modules:[{name:"memory",state,error,dependencies:["orchestrator","embedding_init"]},{name:"book",state:"stopped",error:"",dependencies:["orchestrator"]},{name:"meme",state:"stopped",error:"",dependencies:["orchestrator"]},{name:"feeds",state:"stopped",error:"",dependencies:["orchestrator"]}],busy,error:""});
const show=(transport:Parameters<typeof RuntimeCard>[0]["transport"])=>render(<I18nProvider locale="en"><RuntimeCard transport={transport}/></I18nProvider>);
describe("local runtime modules",()=>{
 it("lists stopped modules without starting any container, even when the main orchestrator is down",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":page()});show(transport);
  await screen.findAllByText("Stopped");
  expect(screen.queryByRole("button",{name:"Start book"})).toBeNull();
  expect(screen.queryByRole("button",{name:"Start meme"})).toBeNull();
  expect(screen.getByRole("button",{name:"Start feeds"})).toBeTruthy();
  expect(calls.map(c=>c.type)).toEqual(["runtime.modules"]);
  expect(screen.queryByRole("button",{name:"Stop memory"})).toBeNull();
 });
 it("starts only the selected module and polls an accepted operation until ready",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":[page(),page("starting",true),page("ready")],"runtime.set":page("starting",true)});show(transport);
  fireEvent.click(await screen.findByRole("button",{name:"Start memory"}));
  await screen.findByText("Starting");
  expect((screen.getByRole("button",{name:"Start feeds"}) as HTMLButtonElement).disabled).toBe(false);
  await screen.findByText("Ready",{}, {timeout:3000});
  expect(calls.filter(c=>c.type==="runtime.set").map(c=>c.payload)).toEqual([{module:"memory",enabled:true}]);
  expect(calls.some(c=>c.type==="services.set")).toBe(false);
 });
 it("keeps the module list on screen while polling a busy operation",async()=>{
  const later=deferred<ReturnType<typeof page>>();
  const {transport,calls}=scriptedTransport(page("starting",true),()=>later.promise);show(transport);
  await screen.findByText("Starting");
  await waitFor(()=>expect(calls).toHaveLength(2),{timeout:3000});
  expect(screen.queryByText("Loading…")).toBeNull();
  expect(screen.getByText("Starting")).toBeTruthy();
  later.resolve(page("ready"));
  await screen.findByText("Ready");
 });
 it("offers the read-only communications module like any other",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":ok({modules:[{name:"communications",state:"stopped",error:"",dependencies:["orchestrator"]}],busy:false,error:""}),"runtime.set":page("starting",true)});show(transport);
  fireEvent.click(await screen.findByRole("button",{name:"Start communications"}));
  await waitFor(()=>expect(calls.filter(c=>c.type==="runtime.set").map(c=>c.payload)).toEqual([{module:"communications",enabled:true}]));
 });
 it("asks before starting a module that posts, and starts nothing on cancel",async()=>{
  const {transport,calls}=routedTransport({"runtime.modules":ok({modules:[{name:"scheduler",state:"stopped",error:"",dependencies:["orchestrator"]}],busy:false,error:""}),"runtime.set":page("starting",true)});
  render(<I18nProvider locale="en"><RuntimeCard transport={transport} startNotice={()=>["meme.dispatch runs every 2h and posts memes to the meme channels on Discord."]}/></I18nProvider>);
  fireEvent.click(await screen.findByRole("button",{name:"Start scheduler"}));
  await screen.findByText("meme.dispatch runs every 2h and posts memes to the meme channels on Discord.");
  fireEvent.click(screen.getByRole("button",{name:"Cancel"}));
  expect(calls.some(c=>c.type==="runtime.set")).toBe(false);
  fireEvent.click(screen.getByRole("button",{name:"Start scheduler"}));
  fireEvent.click(screen.getByRole("button",{name:"Start"}));
  await waitFor(()=>expect(calls.filter(c=>c.type==="runtime.set").map(c=>c.payload)).toEqual([{module:"scheduler",enabled:true}]));
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
 await screen.findByRole("heading",{name:"Modules"});
 await screen.findByRole("button",{name:"Start memory"});
 expect(calls.some(c=>c.type==="ningo.status")).toBe(true);
 expect(calls.filter(c=>c.type==="runtime.set")).toHaveLength(0);
});


it("shows one module row and separates lifecycle from optional request handling", async () => {
  const { App } = await import("../../src/renderer/app.js");
  const { transport, calls } = routedTransport({
    "runtime.modules": page("ready"),
    "ningo.status": ok({ status: "ok", services: { memory: { status: "ok", enabled: true } } }),
    "runtime.set": page(),
    "services.set": ok({ name: "memory", enabled: false }),
  });
  render(<App transport={transport} locale="en" runtimeControls />);
  await screen.findByText("Options for memory");
  expect(screen.getAllByText("memory", { selector: ".service-name" })).toHaveLength(1);
  expect(screen.getAllByRole("heading", { level: 2 }).map((heading) => heading.textContent)).toEqual(["Modules", "Settings", "Inference providers"]);
  expect(screen.queryByRole("heading", { name: "Services" })).toBeNull();
  expect(screen.getByText("Options for memory").closest("details")?.open).toBe(false);
  fireEvent.click(screen.getByText("Options for memory"));
  fireEvent.click(screen.getByRole("switch", { name: "Process requests for memory" }));
  await waitFor(() => expect(calls.filter((call) => call.type === "services.set")).toHaveLength(1));
  expect(calls.filter((call) => call.type === "runtime.set")).toHaveLength(0);
  fireEvent.click(screen.getByRole("button", { name: "Stop memory" }));
  await waitFor(() => expect(calls.filter((call) => call.type === "runtime.set").map((call) => call.payload))
    .toEqual([{ module: "memory", enabled: false }]));
});

it("keeps WhatsApp setup in its module row without duplicating the interface", async () => {
  const { App } = await import("../../src/renderer/app.js");
  const { transport, calls } = routedTransport({
    "runtime.modules": ok({ modules: [{ name: "whatsapp", state: "stopped", error: "", dependencies: [] }], busy: false, error: "" }),
    "ningo.status": ok({ status: "ok", services: { whatsapp: { kind: "interface", status: "unchecked", state: "not_configured", configured: false, enabled: true } } }),
  });
  render(<App transport={transport} locale="en" runtimeControls />);
  const options = await screen.findByText("Options for whatsapp");
  expect(screen.getAllByText("whatsapp")).toHaveLength(1);
  fireEvent.click(options);
  expect(screen.getByText(/WhatsApp has not been set up/)).toBeTruthy();
  expect(screen.getByRole("button", { name: "Start whatsapp" })).toBeTruthy();
  expect(calls.filter((call) => call.type.startsWith("communications.link"))).toHaveLength(0);
});

it("retains service visibility when the runtime controller is unavailable", async () => {
  const { App } = await import("../../src/renderer/app.js");
  const { transport } = routedTransport({
    "runtime.modules": failure("unavailable"),
    "ningo.status": ok({ status: "ok", services: { memory: { status: "ok", enabled: true } } }),
  });
  render(<App transport={transport} locale="en" runtimeControls />);
  expect(await screen.findByText("memory")).toBeTruthy();
  expect(screen.getByRole("switch", { name: "memory switch" })).toBeTruthy();
});

it("shows logs on demand while other modules remain usable", async () => {
 const {transport,calls}=routedTransport({"runtime.modules":page("starting",true),"runtime.logs":ok({module:"memory",text:"embedding model: downloading"}),"runtime.set":page("starting",true)});
 show(transport);
 await screen.findByText("Starting");
 expect(calls.some(c=>c.type==="runtime.logs")).toBe(false);
 const details=screen.getByText("Logs for memory").closest("details")!;
 details.open=true;
 fireEvent(details,new Event("toggle"));
 await screen.findByText("embedding model: downloading");
 fireEvent.click(screen.getByRole("button",{name:"Start feeds"}));
 await waitFor(()=>expect(calls.filter(c=>c.type==="runtime.set").map(c=>c.payload)).toEqual([{module:"feeds",enabled:true}]));
 expect(screen.getByText("embedding model: downloading")).toBeTruthy();
});
