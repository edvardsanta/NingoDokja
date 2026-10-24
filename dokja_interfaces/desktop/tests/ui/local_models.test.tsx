import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { ProvidersCard } from "../../src/renderer/cards/providers.js";
import { WorkspacePanel } from "../../src/renderer/cards/workspace_panel.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { ok, routedTransport } from "./support.js";

const ollama = (overrides: Record<string, unknown> = {}) => ok({ configurationRevision: 3, providers: [{
  id: "ollama", name: "Ollama", enabled: true, declaredModels: [], state: "reachable", reason: "", modelDiscovery: "listed",
  models: ["bge-m3:latest", "qwen3:8b"], modelSizes: { "bge-m3:latest": "1.2 GB", "qwen3:8b": "5.2 GB" }, embeddingModel: "bge-m3:latest",
  inferenceStatus: "not_tested", truncated: false, ...overrides,
}] });
const chat = ok({ configurationRevision: 1, provider: "", model: "" });
const status = (extra: Record<string, unknown> = {}) => ok({ embeddingModel: "bge-m3", ...extra });

function renderProviders(routes: Parameters<typeof routedTransport>[0]) {
  const routed = routedTransport({ "runtime.chat": chat, ...routes });
  render(<I18nProvider locale="en"><WorkspacePanel trail={["System"]}><ProvidersCard transport={routed.transport} /></WorkspacePanel></I18nProvider>);
  return routed;
}

it("lists the engine's models with their sizes, and the embedding model is marked and never offered for chat", async () => {
  renderProviders({ "runtime.providers": ollama(), "runtime.models.status": status() });
  const embedding = (await screen.findByText("bge-m3:latest")).closest("li")!;
  expect(within(embedding).getByText("1.2 GB")).toBeTruthy();
  expect(within(embedding).getByText("Embedding model, for memory and knowledge")).toBeTruthy();
  expect(within(embedding).queryByRole("button")).toBeNull();
  const chatModel = screen.getByText("qwen3:8b").closest("li")!;
  expect(within(chatModel).getByText("5.2 GB")).toBeTruthy();
  expect(within(chatModel).getByRole("button", { name: "Use qwen3:8b of Ollama for chat" })).toBeTruthy();
  expect(within(chatModel).getByRole("button", { name: "Remove qwen3:8b" })).toBeTruthy();
});

it("removes a model through the shell and reads the inventory again", async () => {
  const { calls } = renderProviders({ "runtime.providers": [ollama(), ollama({ models: ["bge-m3:latest"] })], "runtime.models.status": status(), "runtime.models.remove": status() });
  fireEvent.click(await screen.findByRole("button", { name: "Remove qwen3:8b" }));
  await waitFor(() => expect(screen.queryByText("qwen3:8b")).toBeNull());
  expect(calls.filter((call) => call.type === "runtime.models.remove").map((call) => call.payload)).toEqual([{ model: "qwen3:8b" }]);
});

it("offers to start a stopped engine, and asks the controller only on the press", async () => {
  const { calls } = renderProviders({
    "runtime.providers": [ollama({ state: "unavailable", reason: "service_stopped", modelDiscovery: "unavailable", models: [] }), ollama()],
    "runtime.models.status": [status(), status({ engine: "starting" }), status({ engine: "ready" })],
    "runtime.models.engine.start": status({ engine: "starting" }),
  });
  const start = await screen.findByRole("button", { name: "Start Ollama" });
  expect(screen.queryByRole("textbox", { name: "Download a model" })).toBeNull();
  expect(calls.some((call) => call.type === "runtime.models.engine.start")).toBe(false);
  fireEvent.click(start);
  await screen.findByText("Starting Ollama…");
  // When it is ready the inventory is read again and the models appear.
  await screen.findByText("qwen3:8b", {}, { timeout: 3000 });
  expect(calls.filter((call) => call.type === "runtime.models.engine.start")).toHaveLength(1);
});

it("downloads a model by name, shows its progress, can cancel, and lists it when done", async () => {
  const { calls } = renderProviders({
    "runtime.providers": [ollama({ models: ["bge-m3:latest"] }), ollama()],
    "runtime.models.status": [
      status(),
      status({ pull: { model: "qwen3:8b", state: "pulling", phase: "downloading", percent: 42 } }),
      status({ pull: { model: "qwen3:8b", state: "done" } }),
    ],
    "runtime.models.pull": status({ pull: { model: "qwen3:8b", state: "pulling", phase: "starting" } }),
  });
  const field = await screen.findByRole("textbox", { name: "Download a model" });
  const download = screen.getByRole("button", { name: "Download" }) as HTMLButtonElement;
  fireEvent.change(field, { target: { value: "--insecure" } });
  expect(download.disabled).toBe(true);
  fireEvent.change(field, { target: { value: " qwen3:8b " } });
  fireEvent.click(download);
  await screen.findByText("qwen3:8b: downloading");
  expect(screen.getByText("42%")).toBeTruthy();
  expect(screen.getByRole("button", { name: "Cancel download" })).toBeTruthy();
  expect(calls.filter((call) => call.type === "runtime.models.pull").map((call) => call.payload)).toEqual([{ model: "qwen3:8b" }]);
  await screen.findByText("qwen3:8b is downloaded. Use it for chat above.", {}, { timeout: 3000 });
  await screen.findByRole("button", { name: "Use qwen3:8b of Ollama for chat" });
});

it("says plainly why a download failed", async () => {
  renderProviders({ "runtime.providers": ollama(), "runtime.models.status": status({ pull: { model: "nope", state: "failed", error: "the registry has no model by this name" } }) });
  expect((await screen.findByRole("alert")).textContent).toBe("nope was not downloaded: the registry has no model by this name");
});

it("an older controller without the model actions shows the inventory and no engine panel", async () => {
  renderProviders({ "runtime.providers": ollama(), "runtime.models.status": { ok: false, error: { code: "orchestrator", message: "unsupported local runtime event" } } });
  await screen.findByText("qwen3:8b");
  await waitFor(() => expect(screen.queryByRole("textbox", { name: "Download a model" })).toBeNull());
  expect(screen.queryByText(/unsupported/)).toBeNull();
});
