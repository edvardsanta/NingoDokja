import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { KnowledgeCard } from "../../src/renderer/cards/knowledge.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import {
  KNOWLEDGE_STATUS,
  OFF,
  deferred,
  failure,
  hit,
  ok,
  routedTransport,
  search,
} from "./support.js";

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <KnowledgeCard transport={transport} />
    </I18nProvider>,
  );
}

async function ask(query: string) {
  const input = (await screen.findByLabelText(/Ask the base|Perguntar à base/)) as HTMLInputElement;
  fireEvent.change(input, { target: { value: query } });
  fireEvent.click(screen.getByRole("button", { name: /^(Search|Buscar)$/ }));
}

const searches = (calls: Array<{ type: string; payload?: Record<string, unknown> }>) =>
  calls.filter((call) => call.type === "knowledge.search");

describe("the knowledge card", () => {
  it("shows what the base holds and whether similarity works", async () => {
    const { transport, calls } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS) });
    renderCard(transport);

    await screen.findByText("6 documents in 52 passages");
    screen.getByText("Similarity is on (model-1).");
    expect(calls).toEqual([{ type: "knowledge.status", payload: {}, options: { timeoutMs: 15_000 } }]);
  });

  it("says when similarity is off or down and when passages wait for an embedding", async () => {
    const off = routedTransport({ "knowledge.status": ok({ ...KNOWLEDGE_STATUS, embedderReachable: null }) });
    const view = renderCard(off.transport);
    await screen.findByText("Similarity is off: search matches words only.");
    view.unmount();

    const down = routedTransport({
      "knowledge.status": ok({ ...KNOWLEDGE_STATUS, embedderReachable: false, pendingEmbeddings: 4 }),
    });
    renderCard(down.transport);
    await screen.findByText("Similarity is unavailable: search matches words only.");
    screen.getByText("4 passages wait for an embedding.");
  });

  it("says a switched-off base is switched off, with no search box", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(OFF) });
    renderCard(transport);

    await screen.findByText("Switched off.");
    screen.getByText("(paused by the operator)");
    expect(screen.queryByRole("button", { name: "Search" })).toBeNull();
  });

  it("explains a failure of the status", async () => {
    const { transport } = routedTransport({ "knowledge.status": failure("unavailable") });
    renderCard(transport);

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator is not answering. Is it running?");
  });

  it("searches with the words typed, trimmed, and lists the passages", async () => {
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.search": ok(
        search([
          hit({ rank: 1, title: "On free will", heading: "Chapter 2", text: "What we choose is not free of cause." }),
          hit({ rank: 2, title: "A recipe", heading: "", text: "Mix the flour.", score: 0.31, relevant: false, kind: "", tags: [], sourceRef: "" }),
        ]),
      ),
    });
    renderCard(transport);
    await ask("  free will  ");

    await screen.findByText("On free will");
    expect(searches(calls)).toEqual([
      { type: "knowledge.search", payload: { query: "free will", k: 5 }, options: { timeoutMs: 30_000 } },
    ]);
    const first = screen.getByText("On free will").closest("li") as HTMLElement;
    expect(first.dataset.relevant).toBe("true");
    within(first).getByText("Chapter 2");
    within(first).getByText("What we choose is not free of cause.");
    within(first).getByText("score 0.61, note, a, b, ref-1");

    const second = screen.getByText("A recipe").closest("li") as HTMLElement;
    expect(second.dataset.relevant).toBe("false");
    within(second).getByText("weak match, score 0.31");
    expect(screen.queryByText("No relevant match. These are the closest passages.")).toBeNull();
  });

  it("says when nothing is relevant, when nothing matches and when the search is keyword only", async () => {
    const weak = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.search": [
        ok(search([hit({ relevant: false })], { degraded: true, reason: "similarity is down" })),
        ok(search([])),
      ],
    });
    renderCard(weak.transport);

    await ask("anything");
    await screen.findByText("No relevant match. These are the closest passages.");
    screen.getByText("Keyword search only.");
    screen.getByText("similarity is down");

    await ask("something else");
    await screen.findByText("Nothing matches.");
  });

  it("does not search for an empty or blank question", async () => {
    const { transport, calls } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS) });
    renderCard(transport);
    const input = (await screen.findByLabelText("Ask the base")) as HTMLInputElement;
    const button = screen.getByRole("button", { name: "Search" }) as HTMLButtonElement;

    expect(button.disabled).toBe(true);
    fireEvent.change(input, { target: { value: "   " } });
    expect(button.disabled).toBe(true);
    fireEvent.submit(input.closest("form") as HTMLFormElement);
    expect(searches(calls)).toHaveLength(0);
    expect(input.maxLength).toBe(500);
  });

  it("explains a failed search without losing the status", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.search": failure("timeout"),
    });
    renderCard(transport);
    await ask("free will");

    expect((await screen.findByRole("alert")).textContent).toBe("The orchestrator took too long to answer.");
    screen.getByText("6 documents in 52 passages");
  });

  it("shows a search of a service that was switched off in the meantime", async () => {
    const { transport } = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS), "knowledge.search": ok(OFF) });
    renderCard(transport);
    await ask("free will");

    await screen.findByText("Switched off.");
  });

  it("sends one search at a time and keeps the question while it runs", async () => {
    const slow = deferred<TransportResult>();
    const { transport, calls } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.search": () => slow.promise,
    });
    renderCard(transport);

    await ask("first");
    await screen.findByText("Searching…");
    const input = screen.getByLabelText("Ask the base") as HTMLInputElement;
    expect((screen.getByRole("button", { name: "Search" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(input, { target: { value: "second" } });
    fireEvent.submit(input.closest("form") as HTMLFormElement);
    expect(searches(calls)).toHaveLength(1);

    await act(async () => {
      slow.resolve(ok(search([hit({ title: "First answer" })])));
    });
    await screen.findByText("First answer");
    expect(input.value).toBe("second");
  });

  it("shows text from a document as text, never as markup", async () => {
    const hostile = '<img src="x" onerror="window.__owned = true"><script>window.__owned = true</script>';
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.search": ok(search([hit({ title: hostile, text: hostile, heading: hostile, sourceRef: hostile })])),
    });
    const view = renderCard(transport);
    await ask("anything");

    await screen.findAllByText(hostile, { exact: false });
    expect(view.container.querySelector("img")).toBeNull();
    expect(view.container.querySelector("script")).toBeNull();
    expect((window as unknown as { __owned?: boolean }).__owned).toBeUndefined();
  });

  describe("adding", () => {
    const fill = async () => {
      fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "A thought" } });
      fireEvent.change(screen.getByLabelText("Text"), { target: { value: "Something to keep." } });
      fireEvent.click(screen.getByRole("button", { name: "Add" }));
    };
    const statuses = (calls: Array<{ type: string }>) => calls.filter((call) => call.type === "knowledge.status");

    it("is offered under the search, and not when the service is switched off", async () => {
      const on = routedTransport({ "knowledge.status": ok(KNOWLEDGE_STATUS) });
      const view = renderCard(on.transport);
      await screen.findByRole("heading", { name: "Add to the research base" });
      view.unmount();

      renderCard(routedTransport({ "knowledge.status": ok(OFF) }).transport);
      await screen.findByText("Switched off.");
      expect(screen.queryByRole("heading", { name: "Add to the research base" })).toBeNull();
    });

    it("reads the counts again, quietly, once something was added", async () => {
      const { transport, calls } = routedTransport({
        "knowledge.status": [ok(KNOWLEDGE_STATUS), ok({ ...KNOWLEDGE_STATUS, documents: 7, chunks: 55 })],
        "knowledge.ingest": ok({ count: 1, created: 1, updated: 0, unchanged: 0, chunks: 3, degraded: false, reason: "" }),
      });
      renderCard(transport);
      await screen.findByText("6 documents in 52 passages");

      await fill();

      await screen.findByText("7 documents in 55 passages");
      expect(statuses(calls)).toHaveLength(2);
      screen.getByText("Added. Passages stored: 3.");
    });

    it("leaves the counts alone when it was already there", async () => {
      const { transport, calls } = routedTransport({
        "knowledge.status": ok(KNOWLEDGE_STATUS),
        "knowledge.ingest": ok({ count: 1, created: 0, updated: 0, unchanged: 1, chunks: 0, degraded: false, reason: "" }),
      });
      renderCard(transport);
      await screen.findByText("6 documents in 52 passages");

      await fill();

      await screen.findByText("Already in the research base.");
      expect(statuses(calls)).toHaveLength(1);
    });
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = routedTransport({
      "knowledge.status": ok(KNOWLEDGE_STATUS),
      "knowledge.search": ok(search([hit({ relevant: false })])),
    });
    renderCard(transport, "pt");

    await screen.findByText("6 documentos em 52 trechos");
    screen.getByRole("heading", { name: "Base de pesquisa" });
    await ask("livre-arbítrio");
    await screen.findByText("Nenhuma correspondência relevante. Estes são os trechos mais próximos.");
    screen.getByText("correspondência fraca, pontuação 0,61, note, a, b, ref-1");
  });
});
