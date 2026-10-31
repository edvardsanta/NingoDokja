import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KnowledgeWorkspace } from "../../src/renderer/cards/knowledge_workspace.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { DigestStatus } from "../../src/shared/replies.js";
import type { Transport } from "../../src/shared/transport.js";
import { DIGEST_STATUS, KNOWLEDGE_STATUS, OFF, digestItem, digestPage, digestSource, failure, ok, routedTransport } from "./support.js";

const LIST = ok({ documents: [], total: 0, offset: 0 });
const status = (overrides: Partial<DigestStatus>): DigestStatus => ({ ...DIGEST_STATUS, ...overrides });

// Readings is the second view of the Library's workspace: the way a reading is added to it, and where
// the owner finds it.
async function openReadings(transport: Transport) {
  const view = render(<I18nProvider locale="en"><KnowledgeWorkspace transport={transport} /></I18nProvider>);
  fireEvent.click(await screen.findByRole("tab", { name: "Readings" }));
  return view;
}

describe("the Readings view", () => {
  it("is named by its tab: the heading is there for assistive technology only", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1)])) });
    await openReadings(transport);

    const heading = await screen.findByRole("heading", { name: "Readings" });
    expect(heading.className).toBe("sr-only");
    expect(screen.queryByText("Reading digest")).toBeNull();
  });

  it("puts how the sources are doing above the readings, one line, with the list of them open when one fails", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1)])) });
    const view = await openReadings(transport);

    const counts = await screen.findByText("2 working · 1 failing · 1 off");
    const sources = screen.getByText("Sources").closest("details") as HTMLDetailsElement;
    expect(sources.open).toBe(true);
    const list = view.container.querySelector(".digest") as HTMLElement;
    // above the readings, so the one fact that explains a missing reading is not the last on the screen
    expect(counts.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(sources.compareDocumentPosition(list) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(within(sources).getByText("exit status 2")).toBeTruthy();
  });

  it("keeps the list of sources closed while all of them work", async () => {
    const healthy = status({ failed: 0, disabled: 0, sources: [digestSource("alpha", { name: "Alpha source", items: 1 })] });
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(healthy), "digest.items": ok(digestPage([digestItem(1)])) });
    await openReadings(transport);

    await screen.findByText("2 working");
    expect((screen.getByText("Sources").closest("details") as HTMLDetailsElement).open).toBe(false);
  });

  it("puts the two dates away until they are wanted, and the clear button until a filter is set", async () => {
    const { transport, calls } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1)])) });
    const view = await openReadings(transport);
    await screen.findByText("Entry 1");

    const dates = view.container.querySelector("details.reading-dates") as HTMLDetailsElement;
    expect(dates.open).toBe(false);
    expect(within(dates).getByText("Dates")).toBeTruthy();
    expect(dates.contains(screen.getByLabelText("From date (UTC)"))).toBe(true);
    expect(dates.contains(screen.getByLabelText("To date (UTC)"))).toBe(true);
    // the form, the search, the source and Apply stay in the page, with the roles they had
    const form = view.container.querySelector("form.reading-filters") as HTMLFormElement;
    expect(form.contains(screen.getByLabelText("Search readings"))).toBe(true);
    expect(form.contains(screen.getByLabelText("Source"))).toBe(true);
    screen.getByRole("button", { name: "Apply filters" });
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();

    fireEvent.change(screen.getByLabelText("Search readings"), { target: { value: "charity" } });
    screen.getByRole("button", { name: "Clear filters" });
    fireEvent.change(screen.getByLabelText("From date (UTC)"), { target: { value: "2026-10-01" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await waitFor(() => expect(calls.filter((call) => call.type === "digest.items").at(-1)?.payload).toEqual({ limit: 8, offset: 0, query: "charity", from: "2026-10-01" }));
    // dates that are applied are not left hidden
    await waitFor(() => expect(dates.open).toBe(true));
  });

  it("puts what can be done with a reading on one line, and what saving said under it", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.list": LIST,
      "digest.status": ok(DIGEST_STATUS),
      "digest.items": ok(digestPage([digestItem(1)])),
      "knowledge.ingest": ok({ count: 1, created: 1, updated: 0, unchanged: 0, chunks: 3, degraded: false, reason: "" }),
    });
    const view = await openReadings(transport);
    await screen.findByText("Entry 1");

    const row = view.container.querySelector(".digest-actions") as HTMLElement;
    const discuss = within(row).getByRole("button", { name: "Discuss with Ningo: Entry 1" });
    const save = within(row).getByRole("button", { name: "Save to the Library: Entry 1" });
    expect(row.contains(screen.getByText("Preview"))).toBe(true);
    // one action leads: the quieter one is a quiet button
    expect(save.className).toContain("quiet");
    expect(discuss.className).not.toContain("quiet");

    fireEvent.click(save);
    const saved = await screen.findByText("Added. Passages stored: 3.");
    expect(row.contains(saved)).toBe(false);
    expect((saved.closest(".reading-save") as HTMLElement).parentElement?.tagName).toBe("LI");
  });
});

describe("when there is nothing to read", () => {
  it("says what Ningo is reading when nothing is followed, then plainly why", async () => {
    const empty = status({ ok: 0, failed: 0, disabled: 0, sources: [] });
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(empty) });
    await openReadings(transport);

    expect((await screen.findByText("I'm reading nothing right now.")).className).toBe("voice");
    screen.getByText("No source yet. Add a plugin to the feeds service and its items appear here.");
  });

  it("says nothing arrived when the sources work, and a filter that matches nothing only says so", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(DIGEST_STATUS),
      "digest.items": [ok(digestPage([])), ok(digestPage([]))],
    });
    await openReadings(transport);
    expect((await screen.findByText("Nothing has reached me yet.")).className).toBe("voice");
    screen.getByText("Nothing to read yet.");

    fireEvent.change(screen.getByLabelText("Search readings"), { target: { value: "zzz" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply filters" }));
    await screen.findByText("0 matching readings");
    expect(screen.queryByText("Nothing has reached me yet.")).toBeNull();
    expect(screen.queryByText("Nothing to read yet.")).toBeNull();
  });

  it("offers the way to System where the service is off, where the read failed and where the directory cannot be read", async () => {
    const off = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(OFF) });
    const first = await openReadings(off.transport);
    await screen.findByText("Switched off.");
    expect(within(screen.getByRole("tabpanel", { name: "Readings" })).getByRole("button", { name: "Open System" })).toBeTruthy();
    first.unmount();

    const down = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": failure("unavailable") });
    await openReadings(down.transport);
    await screen.findByRole("alert");
    const panel = screen.getByRole("tabpanel", { name: "Readings" });
    within(panel).getByRole("button", { name: "Try again" });
    within(panel).getByRole("button", { name: "Open System" });
  });
});

describe("a file dropped on Readings", () => {
  it("opens the drawer on File, as it does on Documents", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.list": LIST, "digest.status": ok(DIGEST_STATUS), "digest.items": ok(digestPage([digestItem(1)])) });
    await openReadings(transport);
    const reading = await screen.findByText("Entry 1");

    await act(async () => { fireEvent.drop(reading, { dataTransfer: { types: ["Files"], files: [new File(["hello"], "far.md")] } }); });
    await screen.findByText("far.md, 1 KB");
    expect(screen.getByRole("button", { name: "File" }).getAttribute("aria-pressed")).toBe("true");
  });
});
