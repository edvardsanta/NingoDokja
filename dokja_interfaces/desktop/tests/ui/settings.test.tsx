import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { SettingsCard, impactOf } from "../../src/renderer/cards/settings.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { failure, ok, routedTransport } from "./support.js";

// Placeholder ids only.
const settings = (value = "open") => ok({
  busy: false, error: "", webhookChannels: ["102"], settings: [
    { id: "discord.meme_channels", group: "discord", kind: "list", options: [], value: "101,102", affects: ["orchestrator", "communications"], note: "sessions" },
    { id: "discord.direct_messages", group: "discord", kind: "enum", options: ["open", "closed"], value, affects: ["discord"], note: "" },
    { id: "whatsapp.groups", group: "whatsapp", kind: "list", options: [], value: "", affects: ["whatsapp"], note: "" },
  ],
});
const modules = ok({ busy: false, error: "", modules: [{ name: "discord", state: "ready", error: "", dependencies: [] }, { name: "orchestrator", state: "ready", error: "", dependencies: [] }] });
const show = (transport: Parameters<typeof SettingsCard>[0]["transport"]) => render(<I18nProvider locale="en"><SettingsCard transport={transport} /></I18nProvider>);

describe("the settings card", () => {
  it("groups the settings and says whether the bot or a webhook delivers", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    await screen.findByText("101 (bot), 102 (webhook)");
    for (const heading of ["Discord", "WhatsApp"]) expect(screen.getByRole("heading", { name: heading })).toBeTruthy();
    expect(screen.getByText(/DOKJA_DISCORD_WEBHOOKS/)).toBeTruthy();
  });

  it("names what is recreated and saves only after the review", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": [settings(), settings("closed")], "runtime.modules": modules, "runtime.settings.set": settings("closed") });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Edit Direct messages" }));
    fireEvent.change(screen.getByLabelText("Direct messages"), { target: { value: "closed" } });
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    await screen.findByText("Recreated now: discord.");
    expect(calls.some((call) => call.type === "runtime.settings.set")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Save and recreate" }));
    await waitFor(() => expect(calls.filter((call) => call.type === "runtime.settings.set").map((call) => call.payload)).toEqual([{ values: { "discord.direct_messages": "closed" } }]));
  });

  it("cancelling saves nothing", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Edit Direct messages" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(calls.some((call) => call.type === "runtime.settings.set")).toBe(false);
  });

  it("warns that recreating the orchestrator ends chat sessions", () => {
    const impact = impactOf({ id: "discord.meme_channels", group: "discord", kind: "list", options: [], value: "", affects: ["orchestrator", "communications"], note: "sessions", default: "" }, ["orchestrator"]);
    expect(impact).toEqual({ now: ["orchestrator"], later: ["communications"], sessions: true, reindex: false });
  });
});


it("refuses editing when module inspection fails instead of assuming everything is off", async () => {
  const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": failure("unavailable") });
  show(transport);
  await screen.findByRole("alert");
  expect(screen.queryByRole("button", { name: "Edit Direct messages" })).toBeNull();
  expect(calls.some((call) => call.type === "runtime.settings.set")).toBe(false);
});

it("waits for module operations before allowing a settings edit", async () => {
  const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": ok({ modules: [], busy: true }) });
  show(transport);
  expect((await screen.findByRole("button", { name: "Edit Direct messages" }) as HTMLButtonElement).disabled).toBe(true);
});

it("shows the fixed embedding profile without an edit action", async () => {
  const { transport, calls } = routedTransport({
    "runtime.settings": ok({ busy: false, error: "", webhookChannels: [], settings: [
      { id: "models.embedding", group: "models", kind: "text", options: [], value: "", default: "bge-m3", readOnly: true, affects: ["memory", "knowledge"], note: "reindex" },
    ] }), "runtime.modules": modules,
  });
  show(transport);
  await screen.findByText("bge-m3 (default)");
  expect(screen.getByText("Fixed for this version.")).toBeTruthy();
  expect(screen.queryByRole("button", { name: /Edit/ })).toBeNull();
  expect(calls.some((call) => call.type === "runtime.settings.set")).toBe(false);
});
