import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../../src/renderer/app.js";
import { KnowledgeCard } from "../../src/renderer/cards/knowledge.js";
import { KnowledgeWorkspace } from "../../src/renderer/cards/knowledge_workspace.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { ALL_UP, KNOWLEDGE_STATUS, OFF, failure, minutesAgo, ok, routedTransport, statusReply } from "./support.js";

const doc = (id: string, overrides: Record<string, unknown> = {}) => ({
  id, title: `Title ${id}`, kind: "note", reference: "", tags: [] as string[], chunks: 3, embedded: 3, updatedAt: "", ...overrides,
});
const many = (from: number, count: number) => Array.from({ length: count }, (_, index) => doc(`note:${from + index}`));
const listing = (documents: ReturnType<typeof doc>[], total = documents.length, offset = 0) => ok({ documents, total, offset });

function renderCard(transport: Transport, locale: "en" | "pt" = "en") {
  return render(<I18nProvider locale={locale}><KnowledgeCard transport={transport} /></I18nProvider>);
}
function renderLibrary(transport: Transport) {
  return render(<I18nProvider locale="en"><KnowledgeWorkspace transport={transport} /></I18nProvider>);
}
const rows = () => [...document.querySelectorAll<HTMLElement>(".hits.documents > li")];

describe("a row of the list", () => {
  it("is two lines: the title that opens it, then what it is, where it came from, its tags and when it changed", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 1 }),
      "knowledge.list": listing([doc("note:0", { reference: "A book, page 12", tags: ["example", "demo"], updatedAt: minutesAgo(125) })]),
    });
    renderCard(transport);

    const row = (await screen.findByText("Title note:0")).closest("li") as HTMLElement;
    expect(within(row).getByRole("button", { name: "Open Title note:0" })).toBeTruthy();
    // the kind is its own element, with the id in its tooltip; the id is not printed in the row
    expect(within(row).getByText("note").getAttribute("title")).toBe("note:0");
    expect(row.textContent).not.toContain("note:0 ·");
    expect(row.querySelector(".document-meta")?.textContent).toContain("note · A book, page 12 · example, demo");
    expect(within(row).getByText("updated 2 hours ago")).toBeTruthy();
    // indexing is complete: nothing to say about it
    expect(within(row).queryByText(/passages indexed/)).toBeNull();
  });

  it("says how far it is indexed only when it is not all the way, and only where similarity can finish it", async () => {
    const documents = [doc("note:0"), doc("note:1", { chunks: 3, embedded: 0 })];
    const on = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 2 }), "knowledge.list": listing(documents) });
    const view = renderCard(on.transport);
    await screen.findByText("Title note:1");
    expect(screen.getAllByText(/passages indexed\./).map((line) => line.textContent)).toEqual(["0 of 3 passages indexed."]);
    view.unmount();

    // with similarity off every row would be incomplete, and the line above already says so
    const off = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 2, embedderReachable: null }), "knowledge.list": listing(documents) });
    renderCard(off.transport);
    await screen.findByText("Title note:1");
    expect(screen.queryByText(/passages indexed\./)).toBeNull();
  });

  it("leaves out the time when the service gave none or one that is not a date", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 2 }),
      "knowledge.list": listing([doc("note:0", { updatedAt: "" }), doc("note:1", { updatedAt: "not a date" })]),
    });
    renderCard(transport);
    await screen.findByText("Title note:1");
    expect(screen.queryByText(/^updated /)).toBeNull();
  });

  it("speaks Portuguese", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 1 }),
      "knowledge.list": listing([doc("note:0", { updatedAt: minutesAgo(125) })]),
    });
    renderCard(transport, "pt");
    await screen.findByText("Title note:0");
    screen.getByText("atualizado há 2 horas");
  });
});

describe("one tab stop for the list", () => {
  async function renderList(count = 4) {
    const { transport } = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: count }), "knowledge.list": listing(many(0, count)) });
    renderCard(transport);
    await screen.findByText("Title note:0");
  }
  const stops = () => [...document.querySelectorAll<HTMLElement>(".hits.documents button, .hits.documents li")].filter((node) => node.tabIndex === 0);

  it("puts the whole list on one stop of the tab key: the row, not its title or its menu button", async () => {
    await renderList();
    expect(stops()).toEqual([rows()[0]]);
    expect(screen.getByRole("button", { name: "Open Title note:0" }).tabIndex).toBe(-1);
    expect(screen.getByRole("button", { name: "Actions for Title note:0" }).tabIndex).toBe(-1);
  });

  it("moves between rows with the arrows, Home and End, and the stop goes with the focus", async () => {
    await renderList();
    const list = document.querySelector(".hits.documents") as HTMLElement;
    rows()[0]?.focus();

    fireEvent.keyDown(rows()[0] as HTMLElement, { key: "ArrowDown" });
    expect(document.activeElement).toBe(rows()[1]);
    expect(stops()).toEqual([rows()[1]]);
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "End" });
    expect(document.activeElement).toBe(rows()[3]);
    // the ends are ends: the arrows do not wrap
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "ArrowDown" });
    expect(document.activeElement).toBe(rows()[3]);
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "ArrowUp" });
    expect(document.activeElement).toBe(rows()[2]);
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "Home" });
    expect(document.activeElement).toBe(rows()[0]);
    expect(list.querySelectorAll("li[tabindex='0']")).toHaveLength(1);
  });

  it("opens the document with Enter on the row, and Enter on its title still does it once", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [listing(many(0, 3)), listing([doc("note:1", { text: "the text", truncated: false } as never)], 3, 1)],
    });
    renderLibrary(transport);
    await screen.findByText("Title note:1");

    rows()[1]?.focus();
    fireEvent.keyDown(rows()[1] as HTMLElement, { key: "Enter" });
    await screen.findByText("the text");
    expect(screen.getByRole("tab", { name: "Title note:1" }).getAttribute("aria-selected")).toBe("true");
    expect(calls.filter((call) => call.type === "knowledge.list")).toHaveLength(2);
  });

  it("still opens the menu with the Menu key on the row", async () => {
    await renderList();
    rows()[2]?.focus();
    fireEvent.keyDown(rows()[2] as HTMLElement, { key: "ContextMenu" });
    expect(screen.getByRole("menu", { name: "Actions for Title note:2" })).toBeTruthy();
  });
});

describe("the pager", () => {
  it("is not drawn for one page, and says where the owner is when there are more", async () => {
    const one = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 20 }), "knowledge.list": listing(many(0, 20)) });
    const view = renderCard(one.transport);
    await screen.findByText("Title note:0");
    expect(screen.queryByRole("button", { name: "Next documents" })).toBeNull();
    expect(screen.queryByText(/of 20$/)).toBeNull();
    view.unmount();

    const two = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 23 }),
      "knowledge.list": (payload) => Promise.resolve(Number(payload?.offset) === 0 ? listing(many(0, 20), 23) : listing(many(20, 3), 23, 20)),
    });
    renderCard(two.transport);
    await screen.findByText("1–20 of 23");
    fireEvent.click(screen.getByRole("button", { name: "Next documents" }));
    await screen.findByText("21–23 of 23");
    expect((screen.getByRole("button", { name: "Next documents" }) as HTMLButtonElement).disabled).toBe(true);
    // the page the owner left stays until the next one lands, so the pager does not jump
    fireEvent.click(screen.getByRole("button", { name: "Previous documents" }));
    await screen.findByText("1–20 of 23");
  });

  it("never says 'No documents on this page' for a Library with no documents", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 0 }), "knowledge.list": listing([]) });
    renderCard(transport);
    await screen.findByText("The shelf is empty. I can't judge what I haven't read.");
    expect(screen.queryByText("No documents on this page.")).toBeNull();
  });

  it("says so for a page past the end", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 23 }), "knowledge.list": listing([], 23, 40) });
    renderCard(transport);
    await screen.findByText("No documents on this page.");
  });
});

describe("what the count includes that the list leaves out", () => {
  it("explains the difference while the filter is off, and not once it is on or when there is none", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 25 }),
      "knowledge.list": (payload) => Promise.resolve(payload?.external === true ? listing(many(0, 3), 23) : listing(many(0, 3), 25)),
    });
    renderCard(transport);

    await screen.findByText("25 documents in 52 passages");
    screen.getByText("Hidden: 2 that Ningo keeps for himself.");
    // the note is outside the button: the button keeps its name
    const filter = screen.getByRole("button", { name: "Show what Ningo keeps for himself" });
    expect(filter.getAttribute("aria-pressed")).toBe("false");
    fireEvent.click(filter);
    await waitFor(() => expect(filter.getAttribute("aria-pressed")).toBe("true"));
    await waitFor(() => expect(screen.queryByText(/^Hidden:/)).toBeNull());
    // pressed shows a mark as well as a color
    expect(filter.textContent).toContain("✓");
    expect(calls.filter((call) => call.type === "knowledge.list").at(-1)?.payload).toEqual({ limit: 20, offset: 0 });
  });

  it("is silent when the numbers agree or the count is stale and lower", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 3 }), "knowledge.list": listing(many(0, 5)) });
    renderCard(transport);
    await screen.findByText("Title note:0");
    expect(screen.queryByText(/^Hidden:/)).toBeNull();
  });

  it("says nothing was brought in when only what Ningo keeps is there, and lets the owner reveal it", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 4 }),
      "knowledge.list": (payload) => Promise.resolve(payload?.external === true ? listing([], 0) : listing(many(0, 4), 4)),
    });
    renderCard(transport);

    await screen.findByText("The shelf is empty. I can't judge what I haven't read.");
    screen.getByText("Hidden: 4 that Ningo keeps for himself.");
    // no counts, but the search stays: it can match what is kept
    expect(screen.queryByText(/documents in \d+ passages/)).toBeNull();
    expect(screen.getByRole("searchbox")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Show what Ningo keeps for himself" }));
    await screen.findByText("Title note:0");
    expect(screen.queryByText("The shelf is empty. I can't judge what I haven't read.")).toBeNull();
  });
});

describe("the line under the search", () => {
  it("holds the counts, the similarity chip and the filter on one line, and the pending passages with their fix", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 6, pendingEmbeddings: 10 }),
      "knowledge.list": listing(many(0, 6)),
      "knowledge.reindex": ok({ embedded: 10, remaining: 0, degraded: false, reason: "" }),
    });
    const view = renderCard(transport);

    await screen.findByText("6 documents in 52 passages");
    const line = view.container.querySelector(".library-line") as HTMLElement;
    for (const part of ["6 documents in 52 passages", "Similarity on", "10 passages are not indexed yet.", "Show what Ningo keeps for himself"]) {
      expect(within(line).getByText(part)).toBeTruthy();
    }
    fireEvent.click(within(line).getByRole("button", { name: "Index pending passages" }));
    await screen.findByText("10 passages indexed; 0 still pending.");
    expect(calls.filter((call) => call.type === "knowledge.reindex")).toHaveLength(1);
  });

  it("names the search field for assistive technology and shows its words as the placeholder", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": listing(many(0, 6)) });
    renderCard(transport);
    const field = (await screen.findByLabelText("Search the Library")) as HTMLInputElement;
    expect(field.placeholder).toBe("Search the Library");
    expect((document.querySelector(`label[for="${field.id}"]`) as HTMLElement).className).toBe("sr-only");
  });
});

describe("feedback of a deletion", () => {
  it("is announced right under the line, above the list", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 3 }),
      "knowledge.list": listing(many(0, 3)),
      "knowledge.delete": ok({ deleted: true }),
    });
    renderCard(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Actions for Title note:0" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Delete…" }));

    const done = await screen.findByText("Document deleted.");
    const region = done.closest("[aria-live]") as HTMLElement;
    expect(region.getAttribute("aria-live")).toBe("polite");
    const list = document.querySelector(".hits.documents") as HTMLElement;
    // the region comes before the list in the page
    expect(region.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});

describe("when the Library cannot be read", () => {
  it("offers the way out of a failure of the list: try again, or System", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [failure("unavailable"), listing(many(0, 6))],
    });
    renderCard(transport);

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator is not answering. Is it running?");
    screen.getByRole("button", { name: "Open System" });
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText("Title note:0");
    expect(calls.filter((call) => call.type === "knowledge.list")).toHaveLength(2);
  });

  it("offers System where the status failed or the service is off, and keeps the fix out of the way otherwise", async () => {
    const down = routedTransport({ "knowledge.status": failure("unavailable"), "knowledge.list": listing([]) });
    const view = renderCard(down.transport);
    await screen.findByRole("alert");
    screen.getByRole("button", { name: "Try again" });
    screen.getByRole("button", { name: "Open System" });
    view.unmount();

    const off = routedTransport({ "knowledge.status": ok(OFF), "knowledge.list": listing([]) });
    const shut = renderCard(off.transport);
    await screen.findByText("Switched off.");
    screen.getByRole("button", { name: "Open System" });
    expect(screen.queryByRole("searchbox")).toBeNull();
    shut.unmount();

    const fine = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": listing(many(0, 6)) });
    renderCard(fine.transport);
    await screen.findByText("Title note:0");
    expect(screen.queryByRole("button", { name: "Open System" })).toBeNull();
  });

  it("takes the owner to System", async () => {
    const { transport } = routedTransport({
      "ningo.status": statusReply(ALL_UP),
      "conversation.list": ok({ conversations: [], next: "" }),
      "conversation.facts": ok({ facts: [] }),
      "memory.status": ok(OFF),
      "digest.status": ok(OFF),
      "knowledge.status": failure("unavailable"),
    });
    render(<App transport={transport} locale="en" />);
    fireEvent.click(screen.getByRole("tab", { name: "Library" }));
    fireEvent.click(await screen.findByRole("button", { name: "Open System" }));

    await waitFor(() => expect(screen.getByRole("tab", { name: /System/ }).getAttribute("aria-selected")).toBe("true"));
  });
});

describe("Ctrl+F in the Library", () => {
  const press = () => fireEvent.keyDown(document.body, { key: "f", ctrlKey: true });

  it("goes to the search of the Documents, and selects what was typed", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": listing(many(0, 6)) });
    renderLibrary(transport);
    const field = (await screen.findByRole("searchbox")) as HTMLInputElement;
    fireEvent.change(field, { target: { value: "boredom" } });
    (screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement).focus();

    expect(press()).toBe(false);
    expect(document.activeElement).toBe(field);
    expect(field.selectionStart).toBe(0);
    expect(field.selectionEnd).toBe("boredom".length);
  });

  it("comes back from an open document to the search of the Documents", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [listing(many(0, 6)), listing([doc("note:1", { text: "the text", truncated: false } as never)], 6, 1)],
    });
    renderLibrary(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Open Title note:1" }));
    await screen.findByText("the text");

    press();
    await waitFor(() => expect(document.activeElement).toBe(document.querySelector("[data-library-search]")));
    expect(screen.getByRole("tab", { name: "Documents" }).getAttribute("aria-selected")).toBe("true");
  });

  it("is left alone where there is no search, while a dialog is open, and by a Library that is not on screen", async () => {
    const empty = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, documents: 0 }), "knowledge.list": listing([]) });
    const view = renderLibrary(empty.transport);
    await screen.findByText("The shelf is empty. I can't judge what I haven't read.");
    expect(press()).toBe(true);
    view.unmount();

    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": listing(many(0, 6)) });
    render(<I18nProvider locale="en"><div hidden><KnowledgeWorkspace transport={transport} /></div></I18nProvider>);
    await screen.findByText("6 documents in 52 passages");
    expect(press()).toBe(true);
  });

  it("is left to the dialog that is open", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": listing(many(0, 6)) });
    renderLibrary(transport);
    await screen.findByRole("searchbox");
    fireEvent.click(screen.getByRole("button", { name: "Add to the Library" }));
    screen.getByRole("dialog");
    expect(press()).toBe(true);
  });
});

describe("Add in the header", () => {
  it("says Add on the button, and keeps its longer name for assistive technology", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": listing(many(0, 6)) });
    renderLibrary(transport);
    const add = await screen.findByRole("button", { name: "Add to the Library" });
    expect(add.textContent).toBe("Add");
    expect(add.getAttribute("aria-haspopup")).toBe("dialog");
  });
});
