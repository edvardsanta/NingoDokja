import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it } from "vitest";

import { KnowledgeCard } from "../../src/renderer/cards/knowledge.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { KNOWLEDGE_STATUS, ok, routedTransport } from "./support.js";

const listed = (id: string) => ({ id, title: `Title ${id}`, kind: "note", reference: "", tags: [], chunks: 3, embedded: 3, updatedAt: "" });
const PAGE = { documents: [listed("note:zero"), listed("note:one")], total: 2, offset: 0 };
const withText = (id: string) => ({ documents: [{ ...listed(id), text: "the text", truncated: false }], total: 2, offset: 1 });
const CLASSIFIED = { sourceId: "note:one", revision: "a".repeat(64), form: { value: "note", origin: "user" }, contentKind: { value: "reference", origin: "user" }, needsUserInput: false, questions: [] };

function renderCard(transport: Transport) {
  return render(<I18nProvider locale="en"><KnowledgeCard transport={transport} /></I18nProvider>);
}
const row = async (title: string) => (await screen.findByText(title)).closest("li") as HTMLLIElement;
const items = () => screen.getAllByRole("menuitem").map((item) => item.textContent);

it("opens a row's menu with a right click, the Menu key or its ⋯ button, with Open, Classify… and Delete…", async () => {
  const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE) });
  renderCard(transport);
  const second = await row("Title note:one");

  fireEvent.contextMenu(second, { clientX: 40, clientY: 50 });
  expect(screen.getByRole("menu", { name: "Actions for Title note:one" })).toBeTruthy();
  expect(items()).toEqual(["Open", "Classify…", "Delete…"]);
  expect(document.activeElement?.textContent).toBe("Open");
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
  expect(screen.queryByRole("menu")).toBeNull();

  second.focus();
  fireEvent.keyDown(second, { key: "ContextMenu" });
  expect(screen.getByRole("menu")).toBeTruthy();
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
  expect(document.activeElement).toBe(second);

  fireEvent.keyDown(second, { key: "F10", shiftKey: true });
  expect(screen.getByRole("menu")).toBeTruthy();
  fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

  const dots = screen.getByRole("button", { name: "Actions for Title note:one" });
  fireEvent.click(dots);
  expect(dots.getAttribute("aria-expanded")).toBe("true");
  // A press outside closes it.
  fireEvent.pointerDown(document.body);
  expect(screen.queryByRole("menu")).toBeNull();
});

it("moves through the menu with the arrows, wrapping at the ends", async () => {
  const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE) });
  renderCard(transport);
  fireEvent.contextMenu(await row("Title note:one"));
  const menu = screen.getByRole("menu");
  fireEvent.keyDown(menu, { key: "ArrowDown" });
  expect(document.activeElement?.textContent).toBe("Classify…");
  fireEvent.keyDown(menu, { key: "ArrowUp" });
  fireEvent.keyDown(menu, { key: "ArrowUp" });
  expect(document.activeElement?.textContent).toBe("Delete…");
  fireEvent.keyDown(menu, { key: "Home" });
  expect(document.activeElement?.textContent).toBe("Open");
});

it("Classify… opens the document with its classification already shown", async () => {
  const { transport, calls } = routedTransport({
    "knowledge.status": ok(KNOWLEDGE_STATUS),
    "knowledge.list": [ok(PAGE), ok(withText("note:one"))],
    "knowledge.classify": ok(CLASSIFIED),
  });
  renderCard(transport);
  fireEvent.contextMenu(await row("Title note:one"));
  fireEvent.click(screen.getByRole("menuitem", { name: "Classify…" }));
  await screen.findByText("Classification is complete.");
  expect(calls.filter((call) => call.type === "knowledge.classify").map((call) => call.payload)).toEqual([{ source_id: "note:one" }]);
});

it("the title opens the document, as Open in the menu does", async () => {
  const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": [ok(PAGE), ok(withText("note:one"))] });
  renderCard(transport);
  fireEvent.click(await screen.findByRole("button", { name: "Open Title note:one" }));
  expect(await screen.findByText("the text")).toBeTruthy();
});

it("adds to the Library in a drawer that keeps the focus and gives it back when closed", async () => {
  const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE) });
  renderCard(transport);
  const add = await screen.findByRole("button", { name: "Add to the Library" });
  add.focus();
  fireEvent.click(add);
  const drawer = screen.getByRole("dialog", { name: "Add to the Library" });
  expect(drawer.contains(document.activeElement)).toBe(true);
  expect(document.activeElement?.className).not.toBe("drawer-close");
  // One heading: the drawer's, not the form's as well.
  expect(screen.getAllByRole("heading", { name: "Add to the Library" })).toHaveLength(1);

  fireEvent.keyDown(drawer, { key: "Escape" });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  expect(document.activeElement).toBe(add);

  fireEvent.click(add);
  fireEvent.click(screen.getByRole("button", { name: "Close Add to the Library" }));
  expect(screen.queryByRole("dialog")).toBeNull();
});
