import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KnowledgeWorkspace } from "../../src/renderer/cards/knowledge_workspace.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { KNOWLEDGE_STATUS, failure, ok, routedTransport } from "./support.js";

const listed = (id: string) => ({ id, title: `Title ${id}`, kind: "note", reference: "Book, p. 12", tags: ["a"], chunks: 3, embedded: 3, updatedAt: "" });
const PAGE = { documents: [listed("note:zero"), listed("note:one")], total: 2, offset: 0 };
const withText = (id: string, text: string, truncated = false) => ({ documents: [{ ...listed(id), text, truncated }], total: 2, offset: 1 });

function renderCard(transport: Transport) {
  return render(
    <I18nProvider locale="en">
      <KnowledgeWorkspace transport={transport} />
    </I18nProvider>,
  );
}

async function openSecond() {
  fireEvent.click(await screen.findByRole("button", { name: "Open Title note:one" }));
}

// A document is a tab of the strip the views are in, after them.
const strip = () => screen.getByRole("tablist", { name: "Library" });
const tabs = () => within(strip()).getAllByRole("tab").map((tab) => tab.textContent);
const current = () =>
  within(strip())
    .getAllByRole("tab")
    .filter((tab) => tab.getAttribute("aria-selected") === "true")
    .map((tab) => tab.textContent);

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
    expect(tabs()).toEqual(["Documents", "Readings", "Title note:one"]);
    expect(calls.filter((call) => call.type === "knowledge.list").map((call) => call.payload)).toEqual([
      { limit: 20, offset: 0, external: true },
      // a document is read by its id, not by where the list had it
      { source_id: "note:one", include_text: true },
    ]);
  });

  it("keeps the list behind its own tab, goes back to it when the document is closed, and opens once", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok(withText("note:one", "the text"))],
    });
    renderCard(transport);
    await openSecond();
    await screen.findByText("the text");
    expect(screen.getByRole("region", { name: "Library › Documents › Title note:one" })).toBeTruthy();

    fireEvent.click(screen.getByRole("tab", { name: "Documents" }));
    expect(current()).toEqual(["Documents"]);
    expect(screen.getByRole("region", { name: "Library › Documents" })).toBeTruthy();
    // the list is still there, and opening the same document again goes back to its tab
    fireEvent.click(screen.getByRole("button", { name: "Open Title note:one" }));
    expect(current()).toEqual(["Title note:one"]);
    expect(tabs().filter((name) => name === "Title note:one")).toHaveLength(1);
    expect(calls.filter((call) => call.type === "knowledge.list")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Close Title note:one" }));
    expect(tabs()).toEqual(["Documents", "Readings"]);
    expect(current()).toEqual(["Documents"]);
    expect(screen.getByRole("button", { name: "Open Title note:one" })).toBeTruthy();
    expect(screen.getByRole("region", { name: "Library › Documents" })).toBeTruthy();
  });

  it("says so when the document is no longer there, instead of showing another", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok({ documents: [], total: 1, offset: 0 })],
    });
    renderCard(transport);
    await openSecond();

    await screen.findByText("This document is no longer in the Library.");
    expect(screen.queryByText(/another document sits/)).toBeNull();
    // the tab is still there to close
    screen.getByRole("button", { name: "Close Title note:one" });
  });

  it("is not fooled by a reply for another document", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok(withText("note:other", "not this one"))],
    });
    renderCard(transport);
    await openSecond();

    await screen.findByText("This document is no longer in the Library.");
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

describe("a document opened from a search hit", () => {
  it("opens the hit's document in a tab of its own, read by its id", async () => {
    const search = { query: "free", hits: [{ rank: 1, title: "Title note:seven", heading: "", kind: "note", sourceRef: "", sourceId: "note:seven", tags: [], text: "a passage", score: 0.8, relevant: true }], relevantCount: 1, threshold: 0.45, degraded: false, reason: "" };
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok({ documents: [{ ...listed("note:seven"), text: "The whole text.", truncated: false }], total: 1, offset: 0 })],
      "knowledge.search": ok(search),
    });
    renderCard(transport);
    fireEvent.change(await screen.findByRole("searchbox"), { target: { value: "free" } });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    fireEvent.click(await screen.findByRole("button", { name: "Open Title note:seven" }));

    expect(await screen.findByText("The whole text.")).toBeTruthy();
    expect(current()).toEqual(["Title note:seven"]);
    const read = calls.filter((call) => call.type === "knowledge.list").at(-1);
    expect(read?.payload).toEqual({ source_id: "note:seven", include_text: true });
  });

  it("offers no Open for a hit that names no document", async () => {
    const search = { query: "free", hits: [{ rank: 1, title: "Nameless", heading: "", kind: "note", sourceRef: "", sourceId: "", tags: [], text: "a passage", score: 0.8, relevant: true }], relevantCount: 1, threshold: 0.45, degraded: false, reason: "" };
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE), "knowledge.search": ok(search) });
    renderCard(transport);
    fireEvent.change(await screen.findByRole("searchbox"), { target: { value: "free" } });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    await screen.findByText("a passage");
    expect(screen.queryByRole("button", { name: "Open Nameless" })).toBeNull();
  });
});

describe("what a document tab says of the document", () => {
  const rich = { ...listed("note:one"), reference: "notes/freedom.md", tags: ["ethics"], chunks: 2, embedded: 2, updatedAt: new Date(Date.now() - 125 * 60_000).toISOString(), text: "What we call reason.", truncated: false };

  async function open() {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok({ documents: [rich], total: 2, offset: 1 }), ok({ documents: [{ ...rich, text: "Read again." }], total: 2, offset: 1 })],
      "knowledge.classify": ok({ sourceId: "note:one", revision: "a".repeat(64), form: { value: "note", origin: "user" }, contentKind: { value: "reference", origin: "user" }, needsUserInput: false, questions: [] }),
    });
    const view = renderCard(transport);
    await openSecond();
    await screen.findByText("What we call reason.");
    return { view, calls };
  }

  it("says what it is once in one line, with the id only in the tooltip of its title", async () => {
    const { view } = await open();

    const line = view.container.querySelector(".document-line") as HTMLElement;
    expect(line.textContent).toBe("note · notes/freedom.md · ethics · updated 2 hours ago · 2 of 2 passages indexed.");
    expect(screen.getByRole("heading", { name: "Title note:one" }).getAttribute("title")).toBe("note:one");
    expect(line.textContent).not.toContain("note:one");
  });

  it("keeps the rebuilt-text note quiet and under the text, and only where there is a text", async () => {
    const { view } = await open();

    const note = screen.getByText("Rebuilt from the passages the Library stored: close to the original, not a faithful copy of it.");
    expect(note.getAttribute("data-tone")).toBe("off");
    const text = view.container.querySelector(".document-text") as HTMLElement;
    expect(text.compareDocumentPosition(note) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("opens the classification with a button that says whether it is open", async () => {
    await open();
    const button = screen.getByRole("button", { name: "Classification" });
    expect(button.getAttribute("aria-expanded")).toBe("false");
    expect(button.textContent).toContain("▸");

    fireEvent.click(button);
    await screen.findByText("Classification is complete.");
    expect(button.getAttribute("aria-expanded")).toBe("true");
    expect(button.textContent).toContain("▾");
    fireEvent.click(button);
    expect(screen.queryByText("Classification is complete.")).toBeNull();
  });

  it("reads the text again, where it stands, when the panel is refreshed", async () => {
    const { calls } = await open();
    await waitFor(() => expect((screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement).disabled).toBe(false));

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("Read again.");
    // the tab was not closed and the text was not blanked to read it
    expect(screen.getByRole("tab", { name: "Title note:one" }).getAttribute("aria-selected")).toBe("true");
    expect(calls.filter((call) => call.payload?.source_id === "note:one")).toHaveLength(2);
  });
});

describe("a document deleted from the list", () => {
  it("does not outlive its deletion: its tab closes and the list is back", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [ok(PAGE), ok(withText("note:one", "the text")), ok({ documents: [listed("note:zero")], total: 1, offset: 0 })],
      "knowledge.delete": ok({ deleted: true }),
    });
    renderCard(transport);
    await openSecond();
    await screen.findByText("the text");
    fireEvent.click(screen.getByRole("tab", { name: "Documents" }));

    fireEvent.click(await screen.findByRole("button", { name: "Actions for Title note:one" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete…" }));

    await screen.findByText("Document deleted.");
    expect(screen.queryByRole("tab", { name: "Title note:one" })).toBeNull();
    expect(screen.getByRole("tab", { name: "Documents" }).getAttribute("aria-selected")).toBe("true");
  });
});
