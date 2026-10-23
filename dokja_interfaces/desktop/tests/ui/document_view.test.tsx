import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KnowledgeCard } from "../../src/renderer/cards/knowledge.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { KNOWLEDGE_STATUS, failure, ok, routedTransport } from "./support.js";

const listed = (id: string) => ({ id, title: `Title ${id}`, kind: "note", reference: "Book, p. 12", tags: ["a"], chunks: 3, embedded: 3, updatedAt: "" });
const PAGE = { documents: [listed("note:zero"), listed("note:one")], total: 2, offset: 0 };
const withText = (id: string, text: string, truncated = false) => ({ documents: [{ ...listed(id), text, truncated }], total: 2, offset: 1 });

function renderCard(transport: Transport) {
  return render(
    <I18nProvider locale="en">
      <KnowledgeCard transport={transport} />
    </I18nProvider>,
  );
}

async function openSecond() {
  fireEvent.click(await screen.findByRole("button", { name: "Open Title note:one" }));
}

const tabs = () => screen.getByRole("group", { name: "Open documents" });
const current = () =>
  within(tabs())
    .getAllByRole("button")
    .filter((button) => button.getAttribute("aria-current") === "true")
    .map((button) => button.textContent);

describe("a document opened from the research base", () => {
  it("opens in a tab of its own, with its text asked for alone and shown as text", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok(withText("note:one", "<b>first</b> passage\nsecond passage"))],
    });
    const view = renderCard(transport);
    await openSecond();

    const text = await screen.findByText(/first<\/b> passage/);
    expect(text.tagName).toBe("PRE");
    expect(view.container.querySelector(".document-text b")).toBeNull();
    screen.getByText("Rebuilt from the passages the Library stored: close to the original, not a faithful copy of it.");
    expect(current()).toEqual(["Title note:one"]);
    expect(calls.filter((call) => call.type === "knowledge.list").map((call) => call.payload)).toEqual([
      { limit: 20, offset: 0 },
      { limit: 1, offset: 1, include_text: true },
    ]);
  });

  it("keeps the base behind its own tab, goes back to the base when closed, and opens once", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok(withText("note:one", "the text"))],
    });
    renderCard(transport);
    await openSecond();
    await screen.findByText("the text");
    expect(screen.queryByRole("heading", { name: "Documents" })).toBeNull();

    fireEvent.click(within(tabs()).getByRole("button", { name: "Library" }));
    expect(current()).toEqual(["Library"]);
    // the list is still there, and opening the same document again goes back to its tab
    fireEvent.click(screen.getByRole("button", { name: "Open Title note:one" }));
    expect(current()).toEqual(["Title note:one"]);
    expect(within(tabs()).getAllByRole("button", { name: "Title note:one" })).toHaveLength(1);
    expect(calls.filter((call) => call.type === "knowledge.list")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Close Title note:one" }));
    expect(screen.queryByRole("group", { name: "Open documents" })).toBeNull();
    screen.getByRole("heading", { name: "Documents" });
  });

  it("says so when another document sits where this one was, instead of showing it", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok(withText("note:other", "not this one"))],
    });
    renderCard(transport);
    await openSecond();

    await screen.findByText(/another document sits where this one was/);
    expect(screen.queryByText("not this one")).toBeNull();
  });

  it("says when the text was cut", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok(withText("note:one", "x".repeat(1200), true))],
    });
    renderCard(transport);
    await openSecond();

    await screen.findByText("Cut after 1,200 characters.");
  });

  it("asks again for a text that failed, and only then offers to", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), failure("timeout"), ok(withText("note:one", "back again"))],
    });
    renderCard(transport);
    await openSecond();

    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText("back again");
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  });
});

it("loads classification for the opened document only when requested", async () => {
  const { transport, calls } = routedTransport({
    "knowledge.status": ok(KNOWLEDGE_STATUS),
    "knowledge.list": [ok(PAGE), ok(withText("note:one", "the text"))],
    "knowledge.classify": ok({ sourceId: "note:one", revision: "a".repeat(64), form: { value: "note", origin: "user" }, contentKind: { value: "reference", origin: "user" }, needsUserInput: false, questions: [] }),
  });
  renderCard(transport);
  await openSecond();
  await screen.findByText("the text");
  expect(calls.some((call) => call.type === "knowledge.classify")).toBe(false);
  fireEvent.click(screen.getByRole("button", { name: "Classification" }));
  await screen.findByText("Classification is complete.");
  expect(calls.filter((call) => call.type === "knowledge.classify").map((call) => call.payload)).toEqual([{ source_id: "note:one" }]);
});
