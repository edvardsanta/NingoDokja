import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KeptFactsPanel } from "../../src/renderer/cards/kept_facts.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { ok, routedTransport } from "./support.js";

const FACT = { id: "a".repeat(32), owner: true, person: "", content: "Prefers tea.", place: "private", createdAt: "2026-10-30T12:00:00Z" };
const OTHER = { id: "b".repeat(32), owner: false, person: "Ana", content: "<img src=x> likes film", place: "whatsapp", createdAt: "2026-10-30T12:00:00Z" };

function show(transport: Parameters<typeof KeptFactsPanel>[0]["transport"]) {
  return render(<I18nProvider locale="en"><KeptFactsPanel transport={transport} /></I18nProvider>);
}

describe("the kept facts panel", () => {
  it("shows each person's facts as text, with where they were kept", async () => {
    const { transport } = routedTransport({ "conversation.facts": ok({ facts: [OTHER, FACT] }) });
    const { container } = show(transport);
    const you = await screen.findByRole("region", { name: "You" });
    within(you).getByText("Prefers tea.");
    expect(within(you).getByText(/Kept in private/)).toBeTruthy();
    const ana = screen.getByRole("region", { name: "Ana" });
    within(ana).getByText("<img src=x> likes film");
    expect(within(ana).getByText(/Kept in a chat on whatsapp/)).toBeTruthy();
    expect(container.querySelector("img")).toBeNull();
  });

  it("says so when nothing is kept", async () => {
    const { transport } = routedTransport({ "conversation.facts": ok({ facts: [] }) });
    show(transport);
    await screen.findByText("Nothing kept yet.");
  });

  it("deletes the one asked and reads the list again", async () => {
    const { transport, calls } = routedTransport({
      "conversation.facts": [ok({ facts: [FACT, OTHER] }), ok({ facts: [OTHER] })],
      "conversation.fact_delete": ok({ deleted: true }),
    });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Delete Prefers tea." }));
    await act(async () => { await Promise.resolve(); });
    expect(calls.find((call) => call.type === "conversation.fact_delete")?.payload).toEqual({ fact_id: FACT.id });
    await screen.findByRole("region", { name: "Ana" });
    expect(screen.queryByText("Prefers tea.")).toBeNull();
  });

  it("keeps the fact and tells why when the delete fails", async () => {
    const { transport } = routedTransport({ "conversation.facts": ok({ facts: [FACT] }) });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Delete Prefers tea." }));
    await screen.findByRole("alert");
    screen.getByText("Prefers tea.");
  });

  it("stays quiet when the owner declines the question", async () => {
    const { transport } = routedTransport({
      "conversation.facts": ok({ facts: [FACT] }),
      "conversation.fact_delete": { ok: false, error: { code: "cancelled", message: "cancelled" } },
    });
    show(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Delete Prefers tea." }));
    await act(async () => { await Promise.resolve(); });
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
