import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KnowledgeWorkspace } from "../../src/renderer/cards/knowledge_workspace.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import { KNOWLEDGE_STATUS, OFF, deferred, failure, hit, ok, routedTransport, search } from "./support.js";

const listed = (n: number) => ({ id: `note:${n}`, title: `Title ${n}`, kind: "note", reference: "", tags: [], chunks: 3, embedded: 3, updatedAt: "" });
const LIST = ok({ documents: [1, 2, 3, 4, 5, 6].map(listed), total: 6, offset: 0 });

function renderLibrary(transport: Transport) {
  return render(<I18nProvider locale="en"><KnowledgeWorkspace transport={transport} /></I18nProvider>);
}
const field = () => screen.getByRole("searchbox") as HTMLInputElement;
async function ask(query: string) {
  fireEvent.change(await screen.findByRole("searchbox"), { target: { value: query } });
  fireEvent.click(screen.getByRole("button", { name: "Search" }));
}
const FOUND = search([
  hit({ rank: 1, title: "On free will", heading: "Chapter two" }),
  hit({ rank: 2, title: "A recipe", heading: "Method", relevant: false, score: 0.3, sourceId: "note:recipe" }),
  hit({ rank: 3, title: "Nameless", sourceId: "" }),
], { query: "boredom" });

describe("what a search finds", () => {
  it("takes the list's place under the same line, and says how many passages answer what was asked", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "knowledge.search": ok(FOUND) });
    const view = renderLibrary(transport);
    await screen.findByText("Title 1");
    await ask("  boredom ");

    await screen.findByText("3 passages for “boredom”");
    // the list, its filter and its pager are not shown; the line that says how well search works is
    expect(view.container.querySelector(".hits.documents")).toBeNull();
    expect(screen.queryByText("Title 1")).toBeNull();
    expect(screen.queryByRole("button", { name: "Show what Ningo keeps for himself" })).toBeNull();
    expect(screen.getByText("6 documents in 52 passages")).toBeTruthy();
    expect(screen.getByText("Similarity on")).toBeTruthy();
    expect(view.container.querySelectorAll(".hits")).toHaveLength(1);
  });

  it("opens the document from the title of a hit, and offers no title to open for a hit that names none", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [LIST, ok({ documents: [{ ...listed(7), id: "note:example-1", text: "The whole text.", truncated: false }], total: 1, offset: 0 })],
      "knowledge.search": ok(FOUND),
    });
    renderLibrary(transport);
    await ask("boredom");

    // no separate Open button beside the title
    await screen.findByText("3 passages for “boredom”");
    expect(screen.queryByRole("button", { name: "Open" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Open Nameless" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Open On free will" }));

    await screen.findByText("The whole text.");
    expect(screen.getByRole("tab", { name: "On free will" }).getAttribute("aria-selected")).toBe("true");
    expect(calls.filter((call) => call.type === "knowledge.list").at(-1)?.payload).toEqual({ source_id: "note:example-1", include_text: true });
  });

  it("is cleared with Clear search: the field is emptied, the list is back and the focus is on the field", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "knowledge.search": ok(FOUND) });
    renderLibrary(transport);
    await ask("boredom");
    await screen.findByText("3 passages for “boredom”");

    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));

    expect(field().value).toBe("");
    expect(document.activeElement).toBe(field());
    await screen.findByText("Title 1");
    expect(screen.queryByText("3 passages for “boredom”")).toBeNull();
    expect(screen.queryByRole("button", { name: "Clear search" })).toBeNull();
    expect(screen.getByRole("button", { name: "Show what Ningo keeps for himself" })).toBeTruthy();
  });

  it("is cleared by Esc in the field, which a search that is not shown leaves alone", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "knowledge.search": ok(FOUND) });
    renderLibrary(transport);
    await ask("boredom");
    await screen.findByText("3 passages for “boredom”");

    expect(fireEvent.keyDown(field(), { key: "Escape" })).toBe(false);
    expect(field().value).toBe("");
    await screen.findByText("Title 1");
    expect(screen.queryByText("3 passages for “boredom”")).toBeNull();
    // with nothing typed and nothing shown, Esc is not the search's
    expect(fireEvent.keyDown(field(), { key: "Escape" })).toBe(true);
  });

  it("keeps the hits on screen while another question is typed, until it is run", async () => {
    const second = deferred<TransportResult>();
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": LIST,
      "knowledge.search": [ok(FOUND), () => second.promise],
    });
    renderLibrary(transport);
    await ask("boredom");
    await screen.findByText("3 passages for “boredom”");

    fireEvent.change(field(), { target: { value: "something else" } });
    expect(screen.getByText("3 passages for “boredom”")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
    await screen.findByText("Searching…");
    // while it runs the old hits stay, and the field keeps what was typed
    expect(screen.getByText("On free will")).toBeTruthy();
    expect(field().value).toBe("something else");
    expect((screen.getByRole("button", { name: "Search" }) as HTMLButtonElement).disabled).toBe(true);

    await act(async () => second.resolve(ok(search([hit({ title: "Another" })], { query: "something else" }))));
    await screen.findByText("1 passages for “something else”");
    expect(screen.queryByText("On free will")).toBeNull();
  });

  it("says what Ningo has on nothing, in his voice and then plainly, and still offers the way back", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "knowledge.search": ok(search([])) });
    renderLibrary(transport);
    await ask("zzz");

    expect((await screen.findByText("I have nothing on that yet.")).className).toBe("voice");
    screen.getByText("Add a document, or try other words.");
    fireEvent.click(screen.getByRole("button", { name: "Clear search" }));
    await screen.findByText("Title 1");
  });

  it("asks the same question again after a failure", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": LIST,
      "knowledge.search": [failure("timeout"), ok(FOUND)],
    });
    renderLibrary(transport);
    await ask("boredom");

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator took too long to answer.");
    // the line stays: the failure does not take the counts with it
    screen.getByText("6 documents in 52 passages");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText("3 passages for “boredom”");
    expect(calls.filter((call) => call.type === "knowledge.search").map((call) => call.payload?.query)).toEqual(["boredom", "boredom"]);
  });

  it("sends the owner to System when the service was switched off meanwhile", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "knowledge.search": ok(OFF) });
    renderLibrary(transport);
    await ask("boredom");

    await screen.findByText("Switched off.");
    screen.getByRole("button", { name: "Open System" });
  });

  it("keeps Add where it is while a search is shown", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "knowledge.search": ok(FOUND) });
    const view = renderLibrary(transport);
    await ask("boredom");
    await screen.findByText("3 passages for “boredom”");

    const header = view.container.querySelector(".wp-head") as HTMLElement;
    expect(within(header).getByRole("button", { name: "Add to the Library" })).toBeTruthy();
  });

  it("is answered by Ctrl+F from a document: the Documents come back with the field ready", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": [LIST, ok({ documents: [{ ...listed(2), text: "the text", truncated: false }], total: 6, offset: 1 })],
      "knowledge.search": ok(FOUND),
    });
    renderLibrary(transport);
    await ask("boredom");
    await screen.findByText("3 passages for “boredom”");
    fireEvent.click(screen.getByRole("button", { name: "Open On free will" }));
    await waitFor(() => expect(screen.getByRole("tab", { name: "On free will" })).toBeTruthy());

    fireEvent.keyDown(document.body, { key: "f", ctrlKey: true });
    await waitFor(() => expect(document.activeElement).toBe(field()));
    // what was found is still there to go back to
    expect(screen.getByText("3 passages for “boredom”")).toBeTruthy();
  });
});
