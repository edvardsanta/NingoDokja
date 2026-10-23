import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";
import { ProvidersCard } from "../../src/renderer/cards/providers.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { ok, failure, routedTransport } from "./support.js";

it("inspects providers only on demand and never claims inference succeeded", async () => {
 const { transport, calls } = routedTransport({ "runtime.providers": ok({ providers: [
  { id: "codex", name: "Codex", state: "installed", modelDiscovery: "unsupported", models: [], inferenceStatus: "not_tested" },
  { id: "ollama", name: "Ollama", state: "reachable", modelDiscovery: "listed", models: ["fixture/model:latest"], inferenceStatus: "not_tested" },
 ] }) });
 render(<I18nProvider locale="en"><ProvidersCard transport={transport} /></I18nProvider>);
 expect(calls).toHaveLength(0);
 fireEvent.click(screen.getByRole("button", { name: "Inspect providers" }));
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
 render(<I18nProvider locale="en"><ProvidersCard transport={transport} /></I18nProvider>);
 fireEvent.click(screen.getByRole("button", { name: "Inspect providers" }));
 await screen.findByRole("alert");
 expect(calls.map((c) => c.type)).toEqual(["runtime.providers"]);
});


it("distinguishes a stopped stack from an empty reachable model catalog", async () => {
 const { transport, calls } = routedTransport({ "runtime.providers": ok({ providers: [
  { id: "ollama", name: "Ollama", state: "unavailable", reason: "service_stopped", modelDiscovery: "unavailable", models: [], inferenceStatus: "not_tested" },
 ] }) });
 render(<I18nProvider locale="en"><ProvidersCard transport={transport} /></I18nProvider>);
 fireEvent.click(screen.getByRole("button", { name: "Inspect providers" }));
 await screen.findByText("Stack service is stopped");
 expect(screen.queryByText("No local models.")).toBeNull();
 expect(calls.map((c) => c.type)).toEqual(["runtime.providers"]);
});
