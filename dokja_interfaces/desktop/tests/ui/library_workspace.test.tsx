import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KnowledgeWorkspace } from "../../src/renderer/cards/knowledge_workspace.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Transport } from "../../src/shared/transport.js";
import { DIGEST_STATUS, KNOWLEDGE_STATUS, OFF, digestItem, digestPage, failure, ok, routedTransport } from "./support.js";

const listed = (id: string) => ({ id, title: `Title ${id}`, kind: "note", reference: "", tags: [], chunks: 3, embedded: 3, updatedAt: "" });
const PAGE = { documents: [listed("note:zero"), listed("note:one")], total: 2, offset: 0 };
const withText = (id: string) => ({ documents: [{ ...listed(id), text: "the text", truncated: false }], total: 2, offset: 1 });
const files = (list: File[]) => ({ dataTransfer: { types: ["Files"], files: list } });

function renderLibrary(transport: Transport) {
  return render(
    <I18nProvider locale="en">
      <KnowledgeWorkspace transport={transport} />
    </I18nProvider>,
  );
}

const READINGS = { "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1)])) };
const add = () => screen.queryByRole("button", { name: "Add to the Library" });

describe("the Library's workspace", () => {
  it("has Add in its header on both views, and on none when the service is off or unreachable", async () => {
    const on = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE), ...READINGS });
    const view = renderLibrary(on.transport);
    await screen.findByText("6 documents in 52 passages");
    expect(add()).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Readings" }));
    await screen.findByText("Entry 1");
    expect(add()).toBeTruthy();
    // beside the refresh, in the header, whichever view is shown
    expect(view.container.querySelector(".wp-head .wp-actions")?.contains(add())).toBe(true);
    view.unmount();

    const off = renderLibrary(routedTransport({ "knowledge.status": ok(OFF), "knowledge.list": ok(PAGE) }).transport);
    await screen.findByText("Switched off.");
    expect(add()).toBeNull();
    off.unmount();

    renderLibrary(routedTransport({ "knowledge.status": failure("unavailable"), "knowledge.list": ok(PAGE) }).transport);
    await screen.findByRole("alert");
    expect(add()).toBeNull();
  });

  it("opens the drawer from the header on whichever view is shown, and gives the focus back", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE), ...READINGS });
    renderLibrary(transport);
    await screen.findByText("6 documents in 52 passages");
    fireEvent.click(screen.getByRole("tab", { name: "Readings" }));
    await screen.findByText("Entry 1");

    const button = add() as HTMLButtonElement;
    button.focus();
    fireEvent.click(button);
    expect(button.getAttribute("aria-expanded")).toBe("true");
    screen.getByRole("dialog", { name: "Add to the Library" });
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(document.activeElement).toBe(button);
  });

  it("takes a file dropped on Readings too, and opens the drawer on File", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE), ...READINGS });
    const view = renderLibrary(transport);
    await screen.findByText("6 documents in 52 passages");
    fireEvent.click(screen.getByRole("tab", { name: "Readings" }));
    const reading = await screen.findByText("Entry 1");
    const zone = view.container.querySelector(".drop-zone") as HTMLElement;

    fireEvent.dragEnter(reading, files([]));
    expect(zone.hasAttribute("data-dragging")).toBe(true);
    screen.getByText("Drop the file to add it to the Library.");
    fireEvent.drop(reading, files([new File(["hello"], "far.md")]));

    expect(zone.hasAttribute("data-dragging")).toBe(false);
    await screen.findByText("far.md, 1 KB");
    expect(screen.getByRole("button", { name: "File" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("leaves a dropped file alone where there is nothing to add to", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(OFF), "knowledge.list": ok(PAGE) });
    const view = renderLibrary(transport);
    const notice = await screen.findByText("Switched off.");

    fireEvent.dragEnter(notice, files([]));
    expect(view.container.querySelector(".drop-zone[data-dragging]")).toBeNull();
    expect(fireEvent.drop(notice, files([new File(["hello"], "far.md")]))).toBe(true);
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("keeps the open documents when a refresh finds the Library unreachable", async () => {
    const { transport } = routedTransport({
      "knowledge.status": [ok(KNOWLEDGE_STATUS), failure("unavailable"), ok(KNOWLEDGE_STATUS)],
      "knowledge.list": [ok(PAGE), ok(withText("note:one"))],
    });
    renderLibrary(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Open Title note:one" }));
    await screen.findByText("the text");
    await waitFor(() => expect((screen.getByRole("button", { name: "Refresh" }) as HTMLButtonElement).disabled).toBe(false));

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(add()).toBeNull());

    // the document is still open in its tab, with its text; the Documents view says what failed
    expect(screen.getByRole("tab", { name: "Title note:one" })).toBeTruthy();
    expect(screen.getByText("the text")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Documents" }));
    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator is not answering. Is it running?");
  });

  it("asks nothing of the feeds until Readings is opened", async () => {
    const { transport, calls } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": ok(PAGE), ...READINGS });
    renderLibrary(transport);
    await screen.findByText("6 documents in 52 passages");

    expect(calls.map((call) => call.type).sort()).toEqual(["knowledge.list", "knowledge.status"]);
    fireEvent.click(screen.getByRole("tab", { name: "Readings" }));
    await screen.findByText("Entry 1");
    expect(calls.filter((call) => call.type === "digest.status")).toHaveLength(1);
  });
});
