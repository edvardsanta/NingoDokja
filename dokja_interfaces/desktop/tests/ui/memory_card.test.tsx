import { fireEvent, render, screen } from "@testing-library/react";
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

    expect((await screen.findByRole("alert")).textContent).toBe("The reply was not what this card expects.");
  });

  it("refreshes both reads", async () => {
    const { transport, calls } = routedTransport({ "memory.list": NO_EXPERIENCES,
      "memory.status": ok(MEMORY_STATUS),
      "memory.stats": ok(MEMORY_SCORE),
    });
    renderCard(transport);
    await screen.findByText("Skill +0.70: beats the baseline.");

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await screen.findByText("Loading…");
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
});
