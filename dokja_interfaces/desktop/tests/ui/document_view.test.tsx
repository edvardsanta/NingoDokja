import { fireEvent, render, screen, within } from "@testing-library/react";
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
      // the Library lists what came from outside, and a document is asked for among the same
      { limit: 20, offset: 0, external: true },
      { limit: 1, offset: 1, include_text: true, external: true },
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
