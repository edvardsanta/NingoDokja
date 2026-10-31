import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KnowledgeWorkspace } from "../../src/renderer/cards/knowledge_workspace.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { KNOWLEDGE_STATUS, hit, ok, routedTransport, search } from "./support.js";

const listed = (id: string) => ({ id, title: `Title ${id}`, kind: "note", reference: "", tags: [], chunks: 3, embedded: 3, updatedAt: "" });
const PAGE = { documents: [listed("note:zero"), listed("note:one")], total: 2, offset: 0 };
const withText = (id: string, text: string) => ({ documents: [{ ...listed(id), text, truncated: false }], total: 2, offset: 1 });

function renderLibrary(transport: Transport) {
  return render(
    <I18nProvider locale="en">
      <KnowledgeWorkspace transport={transport} />
    </I18nProvider>,
  );
}

const refresh = () => screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement;
const settled = () => waitFor(() => expect(refresh().disabled).toBe(false));

describe("refreshing the Library", () => {
  it("is a read, not a reset: the open document, the search and its hits stay where they are", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": [ok(KNOWLEDGE_STATUS), ok({ ...KNOWLEDGE_STATUS, documents: 7, chunks: 60 })],
      "knowledge.list": [ok(PAGE), ok(withText("note:one", "the text")), ok(PAGE), ok(withText("note:one", "the text"))],
      "knowledge.search": ok(search([hit({ title: "On free will", sourceId: "note:seven" })])),
    });
    renderLibrary(transport);

    // a document open in its tab, then a search with a hit in the Documents behind it
    fireEvent.click(await screen.findByRole("button", { name: "Open Title note:one" }));
    await screen.findByText("the text");
    fireEvent.click(screen.getByRole("tab", { name: "Documents" }));
    const field = (await screen.findByRole("searchbox")) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "free will" } });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    await screen.findByText("On free will");
    fireEvent.click(screen.getByRole("tab", { name: "Title note:one" }));
    await settled();

    fireEvent.click(refresh());
    // the counts followed the quiet read
    await screen.findByText("7 documents in 60 passages");
    await settled();

    expect(screen.getByRole("searchbox", { hidden: true })).toBe(field);
    expect(field.value).toBe("free will");
    // the hits are still the list's replacement
    expect(document.querySelector(".hits.documents")).toBeNull();
    expect(screen.getByText("On free will")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Title note:one" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("button", { name: "Close Title note:one" })).toBeTruthy();
    expect(screen.getByText("the text")).toBeTruthy();
    expect(calls.filter((call) => call.type === "knowledge.status")).toHaveLength(2);
  });

  it("keeps the page of the list and the filter that were chosen", async () => {
    const many = (from: number, count: number) =>
      Array.from({ length: count }, (_, index) => listed(`note:${from + index}`));
    const { transport, calls } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 30 }),
      "knowledge.list": (payload) =>
        Promise.resolve(ok({ documents: many(Number(payload?.offset ?? 0), 20), total: 30, offset: Number(payload?.offset ?? 0) })),
    });
    renderLibrary(transport);

    await screen.findByText("Title note:0");
    fireEvent.click(screen.getByRole("button", { name: "Next documents" }));
    await screen.findByText("Title note:20");
    fireEvent.click(screen.getByRole("button", { name: "Show what Ningo keeps for himself" }));
    await waitFor(() => expect(calls.filter((call) => call.type === "knowledge.list").at(-1)?.payload).toEqual({ limit: 20, offset: 0 }));
    fireEvent.click(screen.getByRole("button", { name: "Next documents" }));
    await screen.findByText("Title note:20");
    await settled();

    fireEvent.click(refresh());
    await waitFor(() => expect(calls.filter((call) => call.type === "knowledge.list")).toHaveLength(5));
    await settled();

    // read again at the page and with the filter the owner had chosen, and not sent back to the start
    expect(calls.filter((call) => call.type === "knowledge.list").at(-1)?.payload).toEqual({ limit: 20, offset: 20 });
    expect(screen.getByRole("button", { name: "Show what Ningo keeps for himself" }).getAttribute("aria-pressed")).toBe("true");
    expect(screen.getByText("Title note:20")).toBeTruthy();
  });
});

describe("the search of the Library", () => {
  it("draws what it finds below the toolbar, and Add stays in the header of the panel", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": ok(PAGE),
      "knowledge.search": ok(search([hit({ title: "On free will" }), hit({ rank: 2, title: "A recipe", relevant: false, score: 0.3 })])),
    });
    const view = renderLibrary(transport);

    const add = await screen.findByRole("button", { name: "Add to the Library" });
    fireEvent.change(await screen.findByRole("searchbox"), { target: { value: "free will" } });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    await screen.findByText("On free will");

    // Add is the panel's own action, beside the refresh: nothing a search returns can move it
    const actions = view.container.querySelector(".wp-actions") as HTMLElement;
    expect(actions.contains(screen.getByRole("button", { name: "Add to the Library" }))).toBe(true);
    expect(screen.getByRole("button", { name: "Add to the Library" })).toBe(add);
    // and the toolbar holds the field and nothing the search returned
    const tools = view.container.querySelector(".library-tools") as HTMLElement;
    expect(tools.contains(screen.getByRole("searchbox"))).toBe(true);
    expect(tools.querySelector(".hits")).toBeNull();
    expect(tools.contains(screen.getByText("On free will"))).toBe(false);
    expect(tools.contains(screen.getByText("A recipe"))).toBe(false);
  });
});
