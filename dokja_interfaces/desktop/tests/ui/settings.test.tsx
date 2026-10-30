import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { conversationAlias } from "../../src/renderer/cards/group_discovery.js";
import { SettingsCard, impactOf } from "../../src/renderer/cards/settings.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { failure, ok, routedTransport } from "./support.js";

// Placeholder ids only.
const settings = (value = "open") => ok({
  busy: false, error: "", webhookChannels: ["102"], settings: [
    { id: "discord.meme_channels", group: "discord", kind: "list", options: [], value: "101,102", affects: ["orchestrator", "communications"], note: "sessions" },
    { id: "discord.direct_messages", group: "discord", kind: "enum", options: ["open", "closed"], value, affects: ["discord"], note: "" },
    { id: "whatsapp.groups", group: "whatsapp", kind: "list", options: [], value: "", affects: ["whatsapp"], note: "" },
    { id: "whatsapp.conversations", group: "whatsapp", kind: "list", options: [], value: "eu=5511900000000", affects: ["whatsapp"], note: "" },
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
    await screen.findByText("Recreated now: Discord.");
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

  it("adds and removes list entries before reviewing a settings save", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "runtime.settings.set": settings() });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Edit Meme delivery channels" }));
    const list = screen.getByLabelText("Meme delivery channels").closest(".list-editor");
    if (!list) throw new Error("Meme delivery channels editor missing");
    fireEvent.click(screen.getByRole("button", { name: "Remove 101" }));
    fireEvent.change(screen.getByLabelText("Meme delivery channels"), { target: { value: "103" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(list.textContent).toContain("102");
    expect(list.textContent).toContain("103");
    expect(list.textContent).not.toContain("101");
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    expect(calls.some((call) => call.type === "runtime.settings.set")).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Save and recreate" }));
    await waitFor(() => expect(calls.filter((call) => call.type === "runtime.settings.set").map((call) => call.payload)).toEqual([{ values: { "discord.meme_channels": "102,103" } }]));
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

it("shows a value from a closed set in words, never as the raw option", async () => {
  const { transport } = routedTransport({ "runtime.settings": settings("open"), "runtime.modules": modules });
  show(transport);
  await screen.findByText("Open");
  expect(screen.queryByText("open")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Edit Direct messages" }));
  const options = [...(screen.getByLabelText("Direct messages") as HTMLSelectElement).options];
  expect(options.map((option) => [option.value, option.textContent])).toEqual([["open", "Open"], ["closed", "Closed"]]);
});

describe("finding the WhatsApp groups to allow", () => {
  const groups = ok({ groups: [
    { id: "120363000000000001@g.us", name: "ningo", members: 1 },
    { id: "120363000000000002@g.us", name: "", members: 14 },
  ] });

  it("lists the account's groups by name and adds the chosen one to the draft, saving nothing yet", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "communications.link.groups": groups });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: /^Edit Groups/ }));
    fireEvent.click(screen.getByRole("button", { name: "Find my groups" }));

    const list = await screen.findByRole("list", { name: "Your groups" });
    expect(list.textContent).toContain("ningo");
    expect(list.textContent).toContain("Unnamed group");
    fireEvent.click(screen.getByRole("button", { name: "Add ningo" }));
    expect(screen.getByText("120363000000000001@g.us")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Add ningo" }) as HTMLButtonElement).disabled).toBe(true);
    expect(calls.find((call) => call.type === "communications.link.groups")?.payload).toEqual({ interface: "whatsapp" });
    expect(calls.filter((call) => call.type === "runtime.settings.set")).toHaveLength(0);
  });

  it("says WhatsApp must be connected when the groups cannot be listed", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "communications.link.groups": failure("orchestrator", "the whatsapp interface could not list its groups; is it connected?") });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: /^Edit Groups/ }));
    fireEvent.click(screen.getByRole("button", { name: "Find my groups" }));
    await screen.findByText("WhatsApp must be linked and connected to list your groups.");
  });

  it("is offered only for the WhatsApp groups", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: /^Edit Meme channels|^Edit .*channels/ }));
    expect(screen.queryByRole("button", { name: "Find my groups" })).toBeNull();
  });
});

describe("naming conversations for Messages", () => {
  const groups = ok({ groups: [{ id: "120363000000000001@g.us", name: "Clube de Leitura!", members: 4 }] });

  it("adds a found group under an alias made from its name, warns about recording, and saves through the setting", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "communications.link.groups": groups, "runtime.settings.set": settings() });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: /^Edit Shown in Messages/ }));
    screen.getByText(/Everything said in these conversations is recorded/);
    fireEvent.click(screen.getByRole("button", { name: "Find my groups" }));
    fireEvent.click(await screen.findByRole("button", { name: "Add Clube de Leitura!" }));
    screen.getByText("clube-de-leitura=120363000000000001@g.us");
    expect((screen.getByRole("button", { name: "Add Clube de Leitura!" }) as HTMLButtonElement).disabled).toBe(true);
    expect(calls.filter((call) => call.type === "runtime.settings.set")).toHaveLength(0);
  });

  it("makes aliases the WhatsApp interface accepts, and never repeats one", () => {
    expect(conversationAlias("Família São João", [])).toBe("familia-sao-joao");
    expect(conversationAlias("ningo", ["ningo"])).toBe("ningo-2");
    expect(conversationAlias("ningo", ["ningo", "ningo-2"])).toBe("ningo-3");
    expect(conversationAlias("🎵🎶", [])).toBe("grupo");
    const long = conversationAlias("x".repeat(80), []);
    expect(long.length).toBeLessThanOrEqual(32);
    expect(long).toMatch(/^[a-z0-9][a-z0-9_-]{0,31}$/);
  });
});
