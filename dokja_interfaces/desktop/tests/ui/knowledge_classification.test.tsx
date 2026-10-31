import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { KnowledgeClassificationPanel } from "../../src/renderer/cards/knowledge_classification.js";
import type { KnowledgeClassification } from "../../src/shared/replies.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { deferred, failure, ok, routedTransport } from "./support.js";

const pending: KnowledgeClassification = {
  sourceId: "study", revision: "a".repeat(64),
  form: { value: null, origin: "user_required", classifierId: "", classifierVersion: "", reason: "classification_missing" },
  contentKind: { value: null, origin: "user_required", classifierId: "", classifierVersion: "", reason: "classification_missing" },
  needsUserInput: true,
  questions: [
    { field: "form", question: "What is the form?", allowedValues: ["book", "article"] },
    { field: "content_kind", question: "What content?", allowedValues: ["research", "reference"] },
  ],
};
const partial: KnowledgeClassification = {
  ...pending, revision: "b".repeat(64), form: { ...pending.form, value: "book", origin: "user" }, questions: pending.questions.slice(1),
};
const complete: KnowledgeClassification = {
  ...partial, revision: "c".repeat(64), contentKind: { ...pending.contentKind, value: "research", origin: "user" }, needsUserInput: false, questions: [],
};
const renderPanel = (transport: Parameters<typeof KnowledgeClassificationPanel>[0]["transport"]) => render(
  <I18nProvider locale="en"><KnowledgeClassificationPanel transport={transport} id="study" /></I18nProvider>,
);

describe("answering classification from a document", () => {
  it("says an axis awaits an answer once, and names who decided only for an axis that has a value", async () => {
    const { transport } = routedTransport({ "knowledge.classify": ok(partial) });
    renderPanel(transport);

    await screen.findByText("Content kind");
    // the form has a value from the owner; the content kind has none
    expect(screen.getAllByText("Awaiting your answer")).toHaveLength(1);
    expect(screen.queryByText(/Awaiting your answer · /)).toBeNull();
    const kind = screen.getByText("Content kind").nextElementSibling as HTMLElement;
    expect(kind.textContent).toBe("Awaiting your answer");
    const form = screen.getByText("Form").nextElementSibling as HTMLElement;
    expect(form.textContent).toBe("book · Your answer");
  });

  it("leaves answers blank and submits only chosen axes with the current revision", async () => {
    const { transport, calls } = routedTransport({ "knowledge.classify": [ok(pending), ok(partial), ok(complete)] });
    renderPanel(transport);
    const form = await screen.findByLabelText("What is the form?");
    expect((form as HTMLSelectElement).value).toBe("");
    expect((screen.getByRole("button", { name: "Save answers" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(form, { target: { value: "book" } });
    fireEvent.click(screen.getByRole("button", { name: "Save answers" }));
    await screen.findByText("Classification updated.");
    expect(screen.queryByLabelText("What is the form?")).toBeNull();
    expect((screen.getByLabelText("What content?") as HTMLSelectElement).value).toBe("");
    fireEvent.change(screen.getByLabelText("What content?"), { target: { value: "research" } });
    fireEvent.click(screen.getByRole("button", { name: "Save answers" }));
    await screen.findByText("Classification is complete.");
    expect(calls.map((call) => call.payload)).toEqual([
      { source_id: "study" },
      { source_id: "study", revision: pending.revision, answers: { form: "book" } },
      { source_id: "study", revision: partial.revision, answers: { content_kind: "research" } },
    ]);
  });

  it("requires a fresh snapshot after a conflict and never resubmits stale answers", async () => {
    const { transport, calls } = routedTransport({ "knowledge.classify": [ok(pending), failure("orchestrator", "classification_conflict: reload questions"), ok(partial)] });
    renderPanel(transport);
    fireEvent.change(await screen.findByLabelText("What is the form?"), { target: { value: "book" } });
    fireEvent.click(screen.getByRole("button", { name: "Save answers" }));
    await screen.findByRole("alert");
    expect((screen.getByRole("button", { name: "Save answers" }) as HTMLButtonElement).disabled).toBe(true);
    expect(calls).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Reload classification" }));
    await waitFor(() => expect(screen.queryByLabelText("What is the form?")).toBeNull());
    expect((await screen.findByLabelText("What content?") as HTMLSelectElement).value).toBe("");
    expect(calls).toHaveLength(3);
  });

  it("sends once while saving and ignores an answer after the panel closes", async () => {
    const saving = deferred<ReturnType<typeof ok>>();
    const { transport, calls } = routedTransport({ "knowledge.classify": [ok(pending), () => saving.promise] });
    const view = renderPanel(transport);
    fireEvent.change(await screen.findByLabelText("What is the form?"), { target: { value: "book" } });
    const button = screen.getByRole("button", { name: "Save answers" });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(calls).toHaveLength(2);
    expect((screen.getByRole("button", { name: "Saving…" }) as HTMLButtonElement).disabled).toBe(true);
    view.unmount();
    saving.resolve(ok(partial));
    expect(screen.queryByText("Classification updated.")).toBeNull();
  });

  it("shows disabled services and rejects a snapshot for a different document", async () => {
    const { transport } = routedTransport({ "knowledge.classify": [ok({ ...pending, sourceId: "other" }), ok({ off: true, reason: "paused" })] });
    renderPanel(transport);
    await screen.findByRole("alert");
    expect(screen.queryByRole("combobox")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    await screen.findByText(/paused/);
    expect(screen.queryByRole("button", { name: "Save answers" })).toBeNull();
  });
});
