import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { PluginsCard } from "../../src/renderer/cards/digest.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { failure, ok, routedTransport } from "./support.js";

it("identifies plugin ownership and distinguishes collection from on-demand processing", async () => {
  const source = { id: "example", name: "Example collection", state: "failed", error: "Collection failed", lastOk: "", items: 0 };
  const { transport, calls } = routedTransport({ "digest.status": ok({ configured: true, sources: [source], providers: [{ ...source, id: "processor", name: "Example processor", state: "pending", error: "" }] }) });
  render(<I18nProvider locale="en"><PluginsCard transport={transport} /></I18nProvider>);
  await screen.findByText("Example collection");
  expect(screen.getByText("Collection failed")).toBeTruthy();
  expect(screen.getByText("Collection plugins").closest("details")?.open).toBe(true);
  expect(screen.getByText("Processing plugins").closest("details")?.open).toBe(true);
  expect(screen.getByText(/These plugins run inside Feeds/)).toBeTruthy();
  expect(screen.getByText(/it does not confirm a successful execution/)).toBeTruthy();
  expect(calls.map(call => call.type)).toEqual(["digest.status"]);
});

it("does not report an empty inventory when plugin status is unavailable", async () => {
  const { transport } = routedTransport({ "digest.status": failure("unavailable") });
  render(<I18nProvider locale="en"><PluginsCard transport={transport} /></I18nProvider>);
  await screen.findByRole("alert");
  expect(screen.queryByText("No plugins reported by Feeds.")).toBeNull();
});
