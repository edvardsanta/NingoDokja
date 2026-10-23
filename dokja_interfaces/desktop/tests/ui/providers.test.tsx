import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";
import { ProvidersCard } from "../../src/renderer/cards/providers.js";
import { WorkspacePanel } from "../../src/renderer/cards/workspace_panel.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { ok, failure, routedTransport } from "./support.js";

it("inspects providers when the view opens and never claims inference succeeded", async () => {
 const { transport, calls } = routedTransport({ "runtime.providers": ok({ providers: [
  { id: "codex", name: "Codex", state: "installed", modelDiscovery: "unsupported", models: [], inferenceStatus: "not_tested" },
  { id: "ollama", name: "Ollama", state: "reachable", modelDiscovery: "listed", models: ["fixture/model:latest"], inferenceStatus: "not_tested" },
 ] }) });
 render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={transport} /></WorkspacePanel></I18nProvider>);
 await screen.findByText("fixture/model:latest");
 expect(screen.getByText("CLI found")).toBeTruthy();
 expect(screen.getByText(/Authentication, quota and inference have not been tested/)).toBeTruthy();
 expect(calls.map((c) => c.type)).toEqual(["runtime.providers"]);
 fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
 await waitFor(() => expect(calls).toHaveLength(2));
 expect(calls.every((c) => c.type === "runtime.providers")).toBe(true);
});

it("keeps failed inspection explicit without starting or retrying services", async () => {
 const { transport, calls } = routedTransport({ "runtime.providers": failure("unavailable") });
 render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={transport} /></WorkspacePanel></I18nProvider>);
 await screen.findByRole("alert");
 expect(calls.map((c) => c.type)).toEqual(["runtime.providers"]);
});


it("distinguishes a stopped stack from an empty reachable model catalog", async () => {
 const { transport, calls } = routedTransport({ "runtime.providers": ok({ providers: [
  { id: "ollama", name: "Ollama", state: "unavailable", reason: "service_stopped", modelDiscovery: "unavailable", models: [], inferenceStatus: "not_tested" },
 ] }) });
 render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={transport} /></WorkspacePanel></I18nProvider>);
 await screen.findByText("Stack service is stopped");
 expect(screen.queryByText("No local models.")).toBeNull();
 expect(calls.map((c) => c.type)).toEqual(["runtime.providers"]);
});

const editable = (revision = 7, enabled = true, declarations = ["fixture/old"]) => ok({ configurationRevision: revision, providers: [
 { id: "codex", name: "Codex", state: "installed", reason: "", enabled, declaredModels: declarations, modelDiscovery: "unsupported", models: [], inferenceStatus: "not_tested" },
] });

it("reviews the registration and reloads saved declarations only after confirmation", async () => {
 const { transport, calls } = routedTransport({ "runtime.chat": ok({configurationRevision:1,provider:"",model:""}), "runtime.providers": [editable(), editable(8, false, ["fixture/new"])], "runtime.providers.configure": ok({ provider: "codex", configurationRevision: 8 }) });
 render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={transport} /></WorkspacePanel></I18nProvider>);
 fireEvent.click(await screen.findByRole("button", { name: "Edit Codex registration" }));
 fireEvent.click(screen.getByLabelText("Discovery enabled"));
 fireEvent.click(screen.getByRole("button", { name: "Remove fixture/old" }));
 fireEvent.change(screen.getByLabelText("Declared models (unverified)"), { target: { value: "fixture/new" } });
 fireEvent.click(screen.getByRole("button", { name: "Add" }));
 fireEvent.click(screen.getByRole("button", { name: "Review" }));
 expect(screen.getByRole("alertdialog", { name: "Save Codex registration" })).toBeTruthy();
 expect(calls.some((call) => call.type === "runtime.providers.configure")).toBe(false);
 fireEvent.click(screen.getByRole("button", { name: "Save registration" }));
 await waitFor(() => expect(calls.filter((call) => call.type === "runtime.providers")).toHaveLength(2));
 expect(calls.filter((call) => call.type === "runtime.providers.configure").map((call) => call.payload)).toEqual([{ configuration_revision: 7, provider: "codex", enabled: false, models: ["fixture/new"] }]);
 await screen.findByText("Disabled for discovery");
 expect(screen.getByText("Declared models (unverified): fixture/new")).toBeTruthy();
});

it("cancels without saving and refuses edits against an older controller", async () => {
 const { transport, calls } = routedTransport({ "runtime.chat": ok({configurationRevision:1,provider:"",model:""}), "runtime.providers": [editable(), editable(0)] });
 render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={transport} /></WorkspacePanel></I18nProvider>);
 fireEvent.click(await screen.findByRole("button", { name: "Edit Codex registration" }));
 fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
 expect(calls.some((call) => call.type === "runtime.providers.configure")).toBe(false);
 fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
 await screen.findByText("Editing requires an updated runtime controller. Rebuild and restart it.");
 expect(screen.queryByRole("button", { name: "Edit Codex registration" })).toBeNull();
});

it("keeps a stale revision explicit without automatically retrying or rebasing the draft", async () => {
 const { transport, calls } = routedTransport({ "runtime.chat": ok({configurationRevision:1,provider:"",model:""}), "runtime.providers": editable(), "runtime.providers.configure": { ok: false, error: { code: "orchestrator", message: "provider configuration changed; reload before saving" } } });
 render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={transport} /></WorkspacePanel></I18nProvider>);
 fireEvent.click(await screen.findByRole("button", { name: "Edit Codex registration" }));
 fireEvent.click(screen.getByRole("button", { name: "Review" }));
 fireEvent.click(screen.getByRole("button", { name: "Save registration" }));
 await screen.findByRole("alert");
 expect(calls.filter((call) => call.type === "runtime.providers.configure")).toHaveLength(1);
 expect(calls.filter((call) => call.type === "runtime.providers")).toHaveLength(1);
 expect(calls.every((call) => ["runtime.providers", "runtime.providers.configure", "runtime.chat"].includes(call.type))).toBe(true);
});

it("requires an explicit declared chat model and preserves a failed selection draft",async()=>{
 const {transport,calls}=routedTransport({"runtime.providers":editable(),"runtime.chat":ok({configurationRevision:3,provider:"",model:""}),"runtime.chat.configure":failure("unavailable")});
 render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={transport}/></WorkspacePanel></I18nProvider>);
 const selection=await screen.findByRole("region",{name:"Chat provider and model"});
 await waitFor(()=>expect(selection.querySelector("button")).toBeTruthy());
 fireEvent.click(selection.querySelector("button")!);
 const select=screen.getByRole("combobox",{name:"Chat provider and model"});
 expect((select as HTMLSelectElement).value).toBe("|");
 fireEvent.change(select,{target:{value:"codex|fixture/old"}});
 fireEvent.click(screen.getByRole("button",{name:"Save chat selection"}));
 await screen.findByRole("alert");
 expect(calls.filter(c=>c.type==="runtime.chat.configure").map(c=>c.payload)).toEqual([{configuration_revision:3,provider:"codex",model:"fixture/old"}]);
 expect((select as HTMLSelectElement).value).toBe("codex|fixture/old");
});
