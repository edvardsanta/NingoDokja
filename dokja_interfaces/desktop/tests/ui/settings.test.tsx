import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { conversationAlias } from "../../src/renderer/cards/group_discovery.js";
import { SettingsCard, impactOf } from "../../src/renderer/cards/settings.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
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
const show = (transport: Parameters<typeof SettingsCard>[0]["transport"], locale: Locale = "en") => render(<I18nProvider locale={locale}><SettingsCard transport={transport} /></I18nProvider>);
const section = async (name: RegExp) => { fireEvent.click(await screen.findByRole("button", { name })); };
const REPLIES = /^Discord · Where Ningo answers/;
const MEMES = /^Discord · Meme delivery/;
const ACCESS = /^WhatsApp · Who can talk to Ningo/;
const MESSAGES = /^WhatsApp · Conversations in Messages/;
const saves = (calls: { type: string; payload?: unknown }[]) => calls.filter((call) => call.type === "runtime.settings.set").map((call) => call.payload);

describe("the settings card", () => {
  it("shows one section per thing to set up, with what it holds and its state", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    const memes = await screen.findByRole("button", { name: /^Discord · Meme delivery Configured/ });
    expect(memes.getAttribute("aria-expanded")).toBe("false");
    expect(memes.textContent).toContain("101 and 102");
    screen.getByRole("button", { name: /^WhatsApp · Who can talk to Ningo/ });
    screen.getByRole("button", { name: /^WhatsApp · Conversations in Messages Configured/ });
  });

  it("says whether the bot or a webhook delivers, inside the section", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    await section(MEMES);
    screen.getByText("delivered by the bot");
    screen.getByText("delivered by webhook");
    expect(screen.getByText(/DOKJA_DISCORD_WEBHOOKS/)).toBeTruthy();
  });

  it("names what is recreated and saves only after the review", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": [settings(), settings("closed")], "runtime.modules": modules, "runtime.settings.set": settings("closed") });
    show(transport);
    await section(REPLIES);
    fireEvent.click(screen.getByRole("radio", { name: "Closed" }));
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    await screen.findByText("Recreated now: Discord.");
    expect(saves(calls)).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Save and recreate" }));
    await waitFor(() => expect(saves(calls)).toEqual([{ values: { "discord.direct_messages": "closed" } }]));
    await waitFor(() => expect(screen.getAllByText("Saved.").length).toBeGreaterThan(0));
  });

  it("saves changes from several sections at once, after one review of all of them", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "runtime.settings.set": settings() });
    show(transport);
    await section(REPLIES);
    fireEvent.click(screen.getByRole("radio", { name: "Closed" }));
    await section(MEMES);
    fireEvent.click(screen.getByRole("button", { name: "Remove 101" }));
    screen.getByText("Unsaved changes: 2");
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    const review = screen.getByRole("region", { name: "Review before saving" });
    expect(review.textContent).toContain("Direct messages");
    expect(review.textContent).toContain("Meme channels");
    fireEvent.click(screen.getByRole("button", { name: "Save and recreate" }));
    await waitFor(() => expect(saves(calls)).toEqual([{ values: { "discord.direct_messages": "closed", "discord.meme_channels": "102" } }]));
  });

  it("discarding saves nothing and clears the draft", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    await section(REPLIES);
    fireEvent.click(screen.getByRole("radio", { name: "Closed" }));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));
    expect((screen.getByRole("radio", { name: "Open" }) as HTMLInputElement).checked).toBe(true);
    expect(screen.queryByRole("button", { name: "Review" })).toBeNull();
    expect(saves(calls)).toEqual([]);
  });

  it("adds and removes list entries, and a removal can be undone before reviewing", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "runtime.settings.set": settings() });
    show(transport);
    await section(MEMES);
    fireEvent.click(screen.getByRole("button", { name: "Remove 101" }));
    screen.getByText("will be removed");
    fireEvent.click(screen.getByRole("button", { name: "Undo removing 101" }));
    expect(screen.queryByText("will be removed")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove 101" }));
    fireEvent.change(screen.getAllByLabelText("Add a channel by id")[0]!, { target: { value: "103" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Add" })[0]!);
    screen.getByText("new");
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    expect(saves(calls)).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Save and recreate" }));
    await waitFor(() => expect(saves(calls)).toEqual([{ values: { "discord.meme_channels": "102,103" } }]));
  });

  it("says why an entry is refused, beside the field, and keeps it out of the draft", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    await section(MEMES);
    const field = screen.getAllByLabelText("Add a channel by id")[0]!;
    fireEvent.change(field, { target: { value: "not-digits" } });
    fireEvent.click(screen.getAllByRole("button", { name: "Add" })[0]!);
    expect((await screen.findByRole("alert")).textContent).toBe("A channel id is made of digits only, up to 20.");
    expect(field.getAttribute("aria-invalid")).toBe("true");
    expect(screen.queryByRole("button", { name: "Review" })).toBeNull();
  });

  it("keeps the draft and says nothing was saved when the save is refused", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "runtime.settings.set": failure("orchestrator", "refused by the controller") });
    show(transport);
    await section(REPLIES);
    fireEvent.click(screen.getByRole("radio", { name: "Closed" }));
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    fireEvent.click(screen.getByRole("button", { name: "Save and recreate" }));
    await screen.findByText("Nothing was saved.", { exact: false });
    expect((screen.getByRole("radio", { name: "Closed" }) as HTMLInputElement).checked).toBe(true);
  });

  it("speaks Portuguese through the catalogs", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport, "pt");
    await screen.findByRole("button", { name: /^Discord · Entrega de memes Configurado/ });
    fireEvent.click(screen.getByRole("button", { name: /^WhatsApp · Quem pode falar com o Ningo/ }));
    screen.getByText("Descobrir meus grupos");
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
  expect(screen.queryByRole("button", { name: REPLIES })).toBeNull();
  expect(saves(calls)).toEqual([]);
});

it("waits for module operations before allowing a change", async () => {
  const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": ok({ modules: [], busy: true }) });
  show(transport);
  await section(REPLIES);
  expect((screen.getByRole("radio", { name: "Closed" }) as HTMLInputElement).disabled).toBe(true);
});

it("shows the fixed embedding profile without any way to change it", async () => {
  const { transport, calls } = routedTransport({
    "runtime.settings": ok({ busy: false, error: "", webhookChannels: [], settings: [
      { id: "models.embedding", group: "models", kind: "text", options: [], value: "", default: "bge-m3", readOnly: true, affects: ["memory", "knowledge"], note: "reindex" },
    ] }), "runtime.modules": modules,
  });
  show(transport);
  await section(/^Models/);
  screen.getByText("bge-m3");
  screen.getByText("Fixed for this version.");
  expect(screen.queryByRole("textbox")).toBeNull();
  expect(saves(calls)).toEqual([]);
});

it("shows a value from a closed set in words, never as the raw option", async () => {
  const { transport } = routedTransport({ "runtime.settings": settings("open"), "runtime.modules": modules });
  show(transport);
  await section(REPLIES);
  expect(screen.getAllByRole("radio").map((radio) => [(radio as HTMLInputElement).value, radio.closest("label")?.textContent])).toEqual([["open", "Open"], ["closed", "Closed"]]);
});

describe("finding the WhatsApp groups to allow", () => {
  const groups = ok({ groups: [
    { id: "120363000000000001@g.us", name: "ningo", members: 1 },
    { id: "120363000000000002@g.us", name: "", members: 14 },
  ] });

  it("lists the account's groups by name and adds the chosen one to the draft, saving nothing yet", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "communications.link.groups": groups });
    show(transport);
    await section(ACCESS);
    fireEvent.click(screen.getByRole("button", { name: "Find my groups" }));

    const list = await screen.findByRole("list", { name: "Your groups" });
    expect(list.textContent).toContain("ningo");
    expect(list.textContent).toContain("Unnamed group");
    fireEvent.click(screen.getByRole("button", { name: "Add ningo" }));
    expect(screen.getAllByText("ningo").length).toBeGreaterThan(1);
    expect(calls.find((call) => call.type === "communications.link.groups")?.payload).toEqual({ interface: "whatsapp" });
    expect(saves(calls)).toEqual([]);
  });

  it("says WhatsApp must be connected when the groups cannot be listed", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "communications.link.groups": failure("orchestrator", "the whatsapp interface could not list its groups; is it connected?") });
    show(transport);
    await section(ACCESS);
    fireEvent.click(screen.getByRole("button", { name: "Find my groups" }));
    await screen.findByText("WhatsApp must be linked and connected to list your groups.");
  });

  it("is offered only where groups are chosen", async () => {
    const { transport } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules });
    show(transport);
    await section(MEMES);
    expect(screen.queryByRole("button", { name: "Find my groups" })).toBeNull();
  });
});

describe("naming conversations for Messages", () => {
  const groups = ok({ groups: [{ id: "120363000000000001@g.us", name: "Clube de Leitura!", members: 4 }] });

  it("adds a found group under an alias made from its name, warns about recording, and saves through the setting", async () => {
    const { transport, calls } = routedTransport({ "runtime.settings": settings(), "runtime.modules": modules, "communications.link.groups": groups, "runtime.settings.set": settings() });
    show(transport);
    await section(MESSAGES);
    screen.getByText(/Everything said in these conversations is recorded/);
    fireEvent.click(screen.getByRole("button", { name: "Find my groups" }));
    fireEvent.click(await screen.findByRole("button", { name: "Add Clube de Leitura!" }));
    fireEvent.click(screen.getByRole("button", { name: "Review" }));
    expect(saves(calls)).toEqual([]);
    fireEvent.click(screen.getByRole("button", { name: "Save and recreate" }));
    await waitFor(() => expect(saves(calls)).toEqual([{ values: { "whatsapp.conversations": "eu=5511900000000,clube-de-leitura=120363000000000001@g.us" } }]));
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
