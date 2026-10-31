import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { MemoryCard } from "../../src/renderer/cards/memory.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport, TransportErrorCode } from "../../src/shared/transport.js";
import { MEMORY_SCORE, MEMORY_STATUS, OFF, failure, ok, routedTransport } from "./support.js";

// The kept facts sit below this card and have their own tests.
const withFacts = (transport: Transport): Transport => ({
  ...transport,
  request: (type, payload, options) => type === "conversation.facts" ? Promise.resolve(ok({ facts: [] })) : transport.request(type, payload, options),
});

function renderCard(plain: Transport, locale: Locale = "en") {
  const transport = withFacts(plain);
  return render(
    <I18nProvider locale={locale}>
      <MemoryCard transport={transport} />
    </I18nProvider>,
  );
}

const text = () => document.body.textContent ?? "";

// The experiences are listed as the Notebook opens; these tests are about the rest of it.
const NO_EXPERIENCES = ok({ experiences: [], total: 0, offset: 0 });
const reads = <T extends { type: string }>(calls: T[]) => calls.filter((call) => call.type !== "memory.list");

describe("the memory card", () => {
  it("reads the status, then the score, and shows both", async () => {
    const { transport, calls } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok(MEMORY_SCORE),
    });
    renderCard(transport);

    await screen.findByText("40 experiences, 12 pending, 25 resolved, 3 expired");
    await screen.findByText("Skill +0.70: beats the baseline.");
    screen.getByText("Similarity is on (model-1).");
    screen.getByText("31 scored, 4 not scored yet.");
    screen.getByText("Brier score, lower is better: prediction 0.081, baseline 0.270.");
    expect(reads(calls).map((call) => [call.type, call.options])).toEqual([
      ["memory.status", { timeoutMs: 15_000 }],
      ["memory.stats", { timeoutMs: 15_000 }],
    ]);
  });

  it("says when similarity is off or unreachable, and when experiences need embedding", async () => {
    const off = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok({ ...MEMORY_STATUS, embeddings: false, embedderReachable: false }),
      "memory.stats": ok(MEMORY_SCORE),
    });
    const view = renderCard(off.transport);
    await screen.findByText("Similarity is off: predictions are only the baseline.");
    view.unmount();

    const down = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok({ ...MEMORY_STATUS, embedderReachable: false, needsReindex: 5 }),
      "memory.stats": ok(MEMORY_SCORE),
    });
    renderCard(down.transport);
    await screen.findByText("Similarity is unavailable: predictions are only the baseline.");
    screen.getByText("5 experiences need embedding for this model.");
  });

  it("gives no verdict while too few predictions are scored", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok({ ...MEMORY_SCORE, scored: 12, minScored: 30, enoughData: false, beatsBaseline: true }),
    });
    renderCard(transport);

    await screen.findByText("Too few to judge: 12 scored, 30 needed.");
    screen.getByText("Brier score, lower is better: prediction 0.081, baseline 0.270.");
    expect(text()).not.toContain("beats the baseline");
    expect(text()).not.toContain("does not beat");
  });

  it("says a prediction that does not beat the baseline does not", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok({ ...MEMORY_SCORE, skill: -0.2, beatsBaseline: false }),
    });
    renderCard(transport);

    await screen.findByText("Skill -0.20: does not beat the baseline.");
  });

  it("says nothing is scored yet", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok({ ...MEMORY_SCORE, scored: 0, enoughData: false }),
    });
    renderCard(transport);

    await screen.findByText("Nothing scored yet: no resolved experience has a prediction.");
  });

  it("does not ask for the score of a memory that is switched off", async () => {
    const { transport, calls } = routedTransport({ "memory.list": NO_EXPERIENCES, "memory.status": ok(OFF) });
    renderCard(transport);

    await screen.findByText("Switched off.");
    screen.getByText("(paused by the operator)");
    expect(calls.map((call) => call.type)).toEqual(["memory.status"]);
  });

  it("keeps the status when only the score fails", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": failure("timeout", "no answer within 15000 ms"),
    });
    renderCard(transport);

    await screen.findByText("40 experiences, 12 pending, 25 resolved, 3 expired");
    await screen.findByText("The score could not be read: no answer within 15000 ms");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  const failures: Array<[TransportErrorCode, string]> = [
    ["unavailable", "The orchestrator is not answering. Is it running?"],
    ["timeout", "The orchestrator took too long to answer."],
  ];
  it.each(failures)("explains a %s failure of the status and offers to try again", async (code, message) => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES, "memory.status": failure(code) });
    renderCard(transport);

    expect((await screen.findByRole("alert")).textContent).toBe(message);
    screen.getByRole("button", { name: "Try again" });
  });

  it("treats a status of the wrong shape as unexpected", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES, "memory.status": ok({ nothing: "here" }) });
    renderCard(transport);

    expect((await screen.findByRole("alert")).textContent).toBe("The reply was not what this screen expects.");
  });

  it("refreshes both reads", async () => {
    const { transport, calls } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok(MEMORY_SCORE),
    });
    renderCard(transport);
    await screen.findByText("Skill +0.70: beats the baseline.");
    // the record below is read once the memory answered, and the panel holds its refresh meanwhile
    const refresh = screen.getByRole<HTMLButtonElement>("button", { name: "Refresh" });
    await waitFor(() => expect(refresh.disabled).toBe(false));

    fireEvent.click(refresh);
    await screen.findAllByText("Loading…");
    await screen.findByText("Skill +0.70: beats the baseline.");
    expect(reads(calls).map((call) => call.type)).toEqual(["memory.status", "memory.stats", "memory.status", "memory.stats"]);
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok({ ...MEMORY_SCORE, scored: 12, enoughData: false }),
    });
    renderCard(transport, "pt");

    await screen.findByText("40 experiências, 12 pendentes, 25 resolvidas, 3 expiradas");
    screen.getByText("Poucas para julgar: 12 pontuadas, 30 necessárias.");
    screen.getByRole("heading", { name: "Memória de experiências" });
    screen.getByText("Caderno");
  });

  it("writes its decimals the way Portuguese does", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok(MEMORY_SCORE),
    });
    renderCard(transport, "pt");

    await screen.findByText("Skill +0,70: supera o baseline.");
    screen.getByText("Brier, menor é melhor: previsão 0,081, baseline 0,270.");
  });

  it("speaks once, as Ningo, when something waits for the owner, and keeps the numbers plain", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES, "memory.status": ok(MEMORY_STATUS), "memory.stats": ok(MEMORY_SCORE) });
    renderCard(transport);

    const voice = await screen.findByText("Some of what I tried waits for your verdict.");
    expect(voice.className).toBe("voice");
    expect(voice.nextElementSibling?.textContent).toBe("40 experiences, 12 pending, 25 resolved, 3 expired");
  });

  it("says nothing was tried yet, and when experiences are recorded", async () => {
    const { transport } = routedTransport({
      "memory.list": NO_EXPERIENCES,
      "memory.status": ok({ ...MEMORY_STATUS, experiences: 0, pending: 0, resolved: 0, expired: 0 }),
      "memory.stats": ok(MEMORY_SCORE),
    });
    renderCard(transport);

    await screen.findByText("Nothing recorded. I haven't tried anything yet.");
    screen.getByText("Experiences are recorded when Ningo suggests something, such as a hashtag for a meme.");
  });

  it("has no voice line when nothing waits", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES, "memory.status": ok({ ...MEMORY_STATUS, pending: 0 }), "memory.stats": ok(MEMORY_SCORE) });
    renderCard(transport);

    await screen.findByText("40 experiences, 0 pending, 25 resolved, 3 expired");
    expect(document.querySelector(".voice")).toBeNull();
  });

  const listed = (state: unknown) => state === "pending"
    ? [{ ref: "fixture:wait", action: "hashtag.suggest", detail: "#Wait", outcome: "" }]
    : [{ ref: "fixture:done", action: "hashtag.suggest", detail: "#Done", outcome: "accepted" }];
  const notebook = () => {
    const { transport, calls } = routedTransport({ "memory.status": ok(MEMORY_STATUS), "memory.stats": ok(MEMORY_SCORE), "memory.get": ok({ found: true, experience: { ref: "fixture:wait", action: "hashtag.suggest", detail: "#Wait", outcome: "", predictedP: 0.5, baselineP: 0.5, matchedScore: null, contextSnippet: "", matchedSnippet: "", contextTruncated: false, createdAt: "", resolvedAt: "", feedback: { code: "hashtag_tag", message: "" } } }), "memory.resolve": ok({ found: true, resolved: true, outcome: "accepted" }) });
    const lists = { pending: 0, resolved: 0 };
    const request: Transport["request"] = (type, payload, options) => {
      if (type !== "memory.list") return transport.request(type, payload, options);
      lists[(payload as { state: "pending" | "resolved" }).state] += 1;
      return Promise.resolve(ok({ experiences: listed((payload as { state: string }).state), total: 1, offset: 0, limit: 20 }));
    };
    return { transport: { ...transport, request }, calls, lists };
  };

  it("opens on what waits, then what it keeps, the record, and the numbers closed last", async () => {
    renderCard(notebook().transport);
    await screen.findByText("Suggested #Wait for a meme");
    await screen.findByText("Suggested #Done for a meme");
    const order = ["Waiting for your verdict", "What Ningo keeps", "Saved experiences", "How the predictions fare"].map((label) => text().indexOf(label));
    expect(order.every((at) => at >= 0)).toBe(true);
    expect([...order].sort((left, right) => left - right)).toEqual(order);
    expect(screen.getByText("How the predictions fare").closest("details")?.open).toBe(false);
  });

  it("shows the numbers open when they could not be read", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES, "memory.status": ok(MEMORY_STATUS), "memory.stats": failure("timeout", "no answer") });
    renderCard(transport);
    const problem = await screen.findByText("The score could not be read: no answer");
    expect(problem.closest("details")?.open).toBe(true);
  });

  it("keeps what Ningo keeps when the memory is switched off, without the record or the numbers", async () => {
    const { transport } = routedTransport({ "memory.status": ok(OFF) });
    renderCard(transport);
    await screen.findByText("Switched off.");
    await screen.findByText("Nothing kept yet.");
    expect(text()).not.toContain("Saved experiences");
    expect(text()).not.toContain("How the predictions fare");
  });

  it("says when nothing waits", async () => {
    const { transport } = routedTransport({ "memory.list": NO_EXPERIENCES, "memory.status": ok({ ...MEMORY_STATUS, pending: 0 }), "memory.stats": ok(MEMORY_SCORE) });
    renderCard(transport);
    await screen.findByText("Nothing waits for your verdict.");
  });

  it("rereads both lists and the counts after a verdict, without blanking the view", async () => {
    const { transport, calls, lists } = notebook();
    renderCard(transport);
    fireEvent.click(await screen.findByRole("button", { name: "Inspect fixture:wait" }));
    await screen.findByText("Suggested #Done for a meme");
    const history = screen.getByRole("region", { name: "Saved experiences" });
    fireEvent.click(await screen.findByRole("button", { name: "Accept result" }));
    fireEvent.click(await screen.findByRole("button", { name: "Refresh memory" }));

    await waitFor(() => expect(lists).toEqual({ pending: 2, resolved: 2 }));
    await waitFor(() => expect(calls.filter((call) => call.type === "memory.status")).toHaveLength(2));
    expect(calls.some((call) => call.type === "memory.resolve")).toBe(true);
    expect(history.isConnected).toBe(true);
  });
});
