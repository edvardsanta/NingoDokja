import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HealthCard } from "../../src/renderer/cards/health.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { PulseProvider } from "../../src/renderer/pulse.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport, TransportErrorCode, TransportResult } from "../../src/shared/transport.js";
import { ALL_UP, SERVICES, deferred, failure, scriptedTransport, statusReply } from "./support.js";

function Card({ transport, locale }: { transport: Transport; locale: Locale }) {
  return (
    <I18nProvider locale={locale}>
      <PulseProvider>
        <HealthCard transport={transport} />
      </PulseProvider>
    </I18nProvider>
  );
}

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(<Card transport={transport} locale={locale} />);
}

function row(name: string): HTMLElement {
  const item = screen.getByText(name).closest("li");
  if (!item) throw new Error(`no row for ${name}`);
  return item;
}

const names = () => [...document.querySelectorAll(".service-name")].map((node) => node.textContent);

describe("the health card", () => {
  it("asks for the status and shows each service with its state", async () => {
    const { transport, calls } = scriptedTransport(statusReply(SERVICES, "degraded"));
    renderCard(transport);

    expect(screen.getByRole("status").textContent).toContain("Loading");
    await screen.findByText("1 up, 2 with problems, 1 switched off, 1 not checked");

    expect(calls).toEqual([{ type: "ningo.status", payload: {}, options: { timeoutMs: 15_000 } }]);
    within(row("chat_ai")).getByText("error");
    within(row("chat_ai")).getByText("connection refused");
    within(row("scheduler")).getByText("stopped");
    within(row("memory")).getByText("switched off");
    within(row("book")).getByText("not checked");
    within(row("meme")).getByText("up");
  });

  it("lists the services in the TUI's order", async () => {
    const { transport } = scriptedTransport(statusReply(SERVICES));
    renderCard(transport);
    await screen.findByText("meme");

    expect(names()).toEqual(["meme", "chat_ai", "book", "memory", "scheduler"]);
  });

  it("draws one segment per service, coloured by its state", async () => {
    const { transport } = scriptedTransport(statusReply(SERVICES));
    renderCard(transport);
    await screen.findByText("meme");

    const tones = [...document.querySelectorAll(".strip span")].map((node) => node.getAttribute("data-tone"));
    expect(tones).toEqual(["ok", "problem", "unchecked", "off", "problem"]);
  });

  it("says when the orchestrator reports no services", async () => {
    const { transport } = scriptedTransport(statusReply({}));
    renderCard(transport);

    await screen.findByText("The orchestrator reported no services.");
  });

  const messages: Array<[TransportErrorCode, string]> = [
    ["unavailable", "The orchestrator is not answering. Is it running?"],
    ["timeout", "The orchestrator took too long to answer."],
    ["denied", "This screen is not allowed to ask for that."],
    ["invalid", "The request was malformed."],
    ["orchestrator", "The orchestrator answered with an error: switched off"],
    ["unexpected", "The reply was not what this card expects."],
  ];
  it.each(messages)("explains a %s failure in plain words", async (code, message) => {
    const { transport } = scriptedTransport(failure(code, "switched off"));
    renderCard(transport);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe(message);
    expect(screen.getByRole("button").textContent).toBe("Try again");
  });

  it("treats an answer of the wrong shape as unexpected", async () => {
    const { transport } = scriptedTransport({ ok: true, result: { nothing: "here" } });
    renderCard(transport);

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("The reply was not what this card expects.");
  });

  it("tries again on request and then shows the services", async () => {
    const { transport, calls } = scriptedTransport(failure("unavailable"), statusReply(ALL_UP));
    renderCard(transport);

    await screen.findByRole("alert");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    await screen.findByText("2 up, 0 with problems, 0 switched off");
    expect(calls).toHaveLength(2);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeTruthy();
  });

  it("cannot be refreshed while it is still loading", async () => {
    const gate = deferred<TransportResult>();
    const { transport } = scriptedTransport(() => gate.promise);
    renderCard(transport);

    const button = screen.getByRole("button") as HTMLButtonElement;
    expect(button.disabled).toBe(true);

    gate.resolve(statusReply(ALL_UP));
    await waitFor(() => expect(button.disabled).toBe(false));
  });

  it("drops an answer that arrives after a newer request was made", async () => {
    const slow = deferred<TransportResult>();
    const first = scriptedTransport(() => slow.promise);
    const second = scriptedTransport(statusReply({ fresh: { status: "ok", detail: "" } }));

    const view = renderCard(first.transport);
    view.rerender(<Card transport={second.transport} locale="en" />);
    await screen.findByText("fresh");

    // act() flushes what React would do with the late answer, so a stale one cannot hide
    await act(async () => {
      slow.resolve(statusReply({ stale: { status: "error", detail: "late" } }));
    });

    expect(names()).toEqual(["fresh"]);
    expect(screen.queryByText("stale")).toBeNull();
  });

  it("speaks Portuguese when asked", async () => {
    const { transport } = scriptedTransport(statusReply(SERVICES));
    renderCard(transport, "pt");

    await screen.findByText("1 no ar, 2 com problemas, 1 desligados, 1 sem verificação");
    screen.getByRole("heading", { name: "Serviços" });
    screen.getByText("saúde");
    within(row("scheduler")).getByText("parado");
    screen.getByRole("button", { name: "Atualizar" });
  });
});
