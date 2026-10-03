import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { KnowledgeCard } from "../../src/renderer/cards/knowledge.js";
import { createFakeTransport } from "../../src/renderer/fake_transport.js";
import { IndexPending, KnowledgeDocumentsPanel } from "../../src/renderer/cards/knowledge_documents.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { deferred, failure, KNOWLEDGE_STATUS, ok, OFF, routedTransport } from "./support.js";
import type { TransportResult } from "../../src/shared/transport.js";

const document = (id = "note:one") => ({ id, title: `Title ${id}`, kind: "note", reference: "", tags: [], chunks: 3, embedded: 1, updatedAt: "" });
const page = (offset = 0, total = 21) => ({ documents: Array.from({ length: offset ? 1 : 20 }, (_, i) => document(`note:${offset + i}`)), total, offset });
const wrap = (child: React.ReactNode) => <I18nProvider locale="en">{child}</I18nProvider>;

describe("document management", () => {
  it("loads only on demand and navigates pages", async () => {
    const { transport, calls } = routedTransport({ "knowledge.list": [ok(page()), ok(page(20)), ok(page())] });
    render(wrap(<KnowledgeDocumentsPanel transport={transport} revision={0} onChanged={() => {}} />));
    expect(calls).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Load documents" }));
    await screen.findByText("Title note:0");
    fireEvent.click(screen.getByRole("button", { name: "Next documents" }));
    await screen.findByText("Title note:20");
    expect((screen.getByRole("button", { name: "Next documents" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Previous documents" }));
    await screen.findByText("Title note:0");
    expect(calls.map((call) => call.payload?.offset)).toEqual([0, 20, 0]);
  });

  it("sends the selected id once, keeps a cancelled document, and refreshes after success", async () => {
    const onChanged = vi.fn();
    const { transport, calls } = routedTransport({
      "knowledge.list": ok(page()),
      "knowledge.delete": [failure("cancelled"), ok({ deleted: true })],
    });
    render(wrap(<KnowledgeDocumentsPanel transport={transport} revision={0} onChanged={onChanged} />));
    fireEvent.click(screen.getByRole("button", { name: "Load documents" }));
    const remove = await screen.findByRole("button", { name: "Delete Title note:0" });
    fireEvent.click(remove);
    await screen.findByRole("alert");
    expect(onChanged).not.toHaveBeenCalled();
    screen.getByText("Title note:0");
    fireEvent.click(remove);
    await screen.findByText("Document deleted.");
    expect(onChanged).toHaveBeenCalledTimes(1);
    expect(calls.filter((call) => call.type === "knowledge.delete").map((call) => call.payload)).toEqual([{ id: "note:0" }, { id: "note:0" }]);
  });

  it("reports a deletion timeout without retrying or removing the displayed document", async () => {
    const { transport, calls } = routedTransport({ "knowledge.list": ok(page()), "knowledge.delete": failure("timeout") });
    render(wrap(<KnowledgeDocumentsPanel transport={transport} revision={0} onChanged={vi.fn()} />));
    fireEvent.click(screen.getByRole("button", { name: "Load documents" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete Title note:0" }));
    expect((await screen.findByRole("alert")).textContent).toContain("Reload documents to check");
    screen.getByText("Title note:0");
    expect(calls.filter((call) => call.type === "knowledge.delete")).toHaveLength(1);
  });

  it("handles an off service and a malformed list", async () => {
    const { transport } = routedTransport({ "knowledge.list": [ok(OFF), ok({ documents: "bad" })] });
    render(wrap(<KnowledgeDocumentsPanel transport={transport} revision={0} onChanged={vi.fn()} />));
    fireEvent.click(screen.getByRole("button", { name: "Load documents" }));
    await screen.findByText("Switched off.");
    fireEvent.click(screen.getByRole("button", { name: "Load documents" }));
    await screen.findByRole("alert");
    expect(screen.queryByRole("button", { name: /^Delete / })).toBeNull();
  });

  it("runs only one indexing batch per click, disables it while waiting and reports remaining work", async () => {
    const answer = deferred<TransportResult>();
    const onChanged = vi.fn();
    const { transport, calls } = routedTransport({ "knowledge.reindex": () => answer.promise });
    render(wrap(<IndexPending transport={transport} pending={40} onChanged={onChanged} />));
    const button = screen.getByRole("button", { name: "Index pending passages" });
    fireEvent.click(button);
    fireEvent.click(button);
    expect((button as HTMLButtonElement).disabled).toBe(true);
    answer.resolve(ok({ embedded: 32, remaining: 8, degraded: false, reason: "" }));
    await screen.findByText("32 passages indexed; 8 still pending.");
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(calls).toHaveLength(1);
    expect(calls[0]?.payload).toEqual({ limit: 32 });
  });
});


describe("document management in the knowledge card", () => {
  it("returns to the first page and refreshes counts after removing the last document of a page", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": [ok({ ...KNOWLEDGE_STATUS, documents: 21 }), ok({ ...KNOWLEDGE_STATUS, documents: 20 })],
      "knowledge.list": [ok(page()), ok(page(20)), ok(page(0, 20))],
      "knowledge.delete": ok({ deleted: true }),
    });
    render(wrap(<KnowledgeCard transport={transport} />));
    fireEvent.click(await screen.findByRole("button", { name: "Load documents" }));
    await screen.findByText("Title note:0");
    fireEvent.click(screen.getByRole("button", { name: "Next documents" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete Title note:20" }));
    await screen.findByText("20 documents in 52 passages");
    await screen.findByText("Title note:0");
    expect(screen.queryByText("Title note:20")).toBeNull();
    expect(calls.filter((call) => call.type === "knowledge.list").map((call) => call.payload?.offset)).toEqual([0, 20, 0]);
  });

  it("keeps the browser preview counts consistent with deletion and indexing", async () => {
    render(wrap(<KnowledgeCard transport={createFakeTransport(0)} />));
    fireEvent.click(await screen.findByRole("button", { name: "Load documents" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete An example document, number 1" }));
    await screen.findByText("Document deleted.");
    await waitFor(() => expect(screen.queryByText("An example document, number 1")).toBeNull());
    await screen.findByText("22 documents in the base.");
    fireEvent.click(screen.getByRole("button", { name: "Index pending passages" }));
    await screen.findByText(/passages indexed; 0 still pending/);
    await waitFor(() => expect(screen.queryByRole("button", { name: "Index pending passages" })).toBeNull());
  });
});
