import { fireEvent, render, screen, within, waitFor } from "@testing-library/react";
import {expect,it} from "vitest";
import {TaskWorkbench} from "../../src/renderer/cards/task_workbench.js";
import {I18nProvider} from "../../src/renderer/i18n/context.js";
import {ok,routedTransport} from "./support.js";
it("loads task metadata on expansion and saves explicitly ordered profile choices",async()=>{
 const {transport,calls}=routedTransport({"runtime.tasks.profiles":ok({configurationRevision:3,profiles:{lightweight:[],analysis:[]}}),"runtime.tasks.diagnostics":ok({active:1,queued:2,limit:2,attempts:[{taskId:7,profile:"analysis",provider:"kimi",model:"fixture",durationMs:123,outcome:"failed",reason:"contract_rejected",cause:"",stage:"generation",startedAt:"2026-11-11T10:00:00Z",retryAfterMs:0}],providers:[{provider:"kimi",model:"fixture",retryAfterMs:60000,failureReason:"usage_limit"}]}),"runtime.tasks.configure":ok({configurationRevision:4,profiles:{lightweight:[],analysis:[{provider:"kimi",model:"fixture"}]}})});
 render(<I18nProvider locale="en"><TaskWorkbench transport={transport} providers={[{id:"kimi",name:"Kimi",enabled:true,declaredModels:["fixture"],models:[],state:"installed",reason:"",modelDiscovery:"unsupported",inferenceStatus:"not_tested",truncated:false}]}/></I18nProvider>);
 expect(calls).toHaveLength(0);
 const details=screen.getByText("Tasks and fallback").closest("details")!;details.open=true;fireEvent(details,new Event("toggle"));
 await screen.findByText("Response rejected by contract");expect(screen.getByText("Paused for 60s")).toBeTruthy();expect(screen.getByRole("status").textContent).toContain("2 queued");
 fireEvent.click(screen.getByRole("button",{name:"Edit"}));
 const analysis=screen.getByRole("group",{name:"Complex analysis"});
 fireEvent.change(within(analysis).getByRole("combobox"),{target:{value:"kimi|fixture"}});fireEvent.click(within(analysis).getByRole("button",{name:"Add"}));
 fireEvent.click(screen.getByRole("button",{name:"Save priorities"}));
 await waitFor(()=>expect(calls.some(c=>c.type==="runtime.tasks.configure")).toBe(true));
 expect(calls.find(c=>c.type==="runtime.tasks.configure")?.payload).toEqual({configuration_revision:3,profiles:{lightweight:[],analysis:[{provider:"kimi",model:"fixture"}]}});
});
