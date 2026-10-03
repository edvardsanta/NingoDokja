import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HealthCard } from "../../src/renderer/cards/health.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { PulseProvider } from "../../src/renderer/pulse.js";
import type { JobState } from "../../src/shared/replies.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport, TransportResult } from "../../src/shared/transport.js";
import { deferred, failure, minutesAgo, ok, routedTransport } from "./support.js";

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <PulseProvider>
        <HealthCard transport={transport} />
      </PulseProvider>
    </I18nProvider>,
  );
}

const job = (name: string, overrides: Partial<JobState> = {}): JobState => ({
  name, enabled: true, interval: "6h0m0s", intervalOverride: false, nextAt: "", lastAt: "", lastOutcome: "", lastError: "", ...overrides,
});

const UP = {
  meme: { status: "ok", detail: "", enabled: true },
  scheduler: { status: "ok", detail: "rodando", enabled: true },
  book: { status: "ok", detail: "" },
};
const MEME_OFF = { ...UP, meme: { status: "disabled", detail: "", enabled: false } };
const status = (services: Record<string, unknown>, jobs?: JobState[]) => ok({ status: "ok", services, ...(jobs ? { jobs } : {}) });

const switchOf = (name: string) => screen.getByRole("switch", { name: `${name} switch` }) as HTMLButtonElement;
const changes = (calls: Array<{ type: string; payload?: Record<string, unknown> }>) =>
  calls.filter((call) => call.type !== "ningo.status").map((call) => [call.type, call.payload]);

describe("the switches on the health card", () => {
  it("gives a switch to each service the orchestrator can switch, and none to the rest", async () => {
    const { transport } = routedTransport({ "ningo.status": status(UP) });
    renderCard(transport);
    await screen.findByText("meme");

    expect(screen.getAllByRole("switch").map((node) => node.getAttribute("aria-label"))).toEqual(["meme switch", "scheduler switch"]);
    expect(switchOf("meme").getAttribute("aria-checked")).toBe("true");
    expect(switchOf("meme").textContent).toBe("on");
  });

  it("shows a switched-off service as off", async () => {
    const { transport } = routedTransport({ "ningo.status": status(MEME_OFF) });
    renderCard(transport);
    await screen.findByText("meme");

    expect(switchOf("meme").getAttribute("aria-checked")).toBe("false");
    expect(switchOf("meme").textContent).toBe("off");
  });

  it("asks for the opposite of what it shows, then reads the status again instead of trusting its own guess", async () => {
    const { transport, calls } = routedTransport({
      "ningo.status": [status(UP), status(MEME_OFF)],
      "services.set": ok({ name: "meme", enabled: false }),
    });
    renderCard(transport);
    await screen.findByText("meme");

    fireEvent.click(switchOf("meme"));

    await waitFor(() => expect(switchOf("meme").getAttribute("aria-checked")).toBe("false"));
    expect(calls.map((call) => call.type)).toEqual(["ningo.status", "services.set", "ningo.status"]);
    expect(changes(calls)).toEqual([["services.set", { name: "meme", enabled: false }]]);
  });

  it("shows what the orchestrator holds, even when it did not do what was asked", async () => {
    const { transport, calls } = routedTransport({
      "ningo.status": [status(UP), status(UP)],
      "services.set": ok({ name: "meme", enabled: false }),
    });
    renderCard(transport);
    await screen.findByText("meme");

    fireEvent.click(switchOf("meme"));

    await waitFor(() => expect(calls.filter((call) => call.type === "ningo.status")).toHaveLength(2));
    await waitFor(() => expect(switchOf("meme").getAttribute("aria-checked")).toBe("true"));
  });

  it("makes every switch wait while a change is on its way", async () => {
    const gate = deferred<TransportResult>();
    const { transport } = routedTransport({
      "ningo.status": [status(UP), status(MEME_OFF)],
      "services.set": () => gate.promise,
    });
    renderCard(transport);
    await screen.findByText("meme");

    fireEvent.click(switchOf("meme"));

    await waitFor(() => expect(switchOf("scheduler").disabled).toBe(true));
    expect(switchOf("meme").disabled).toBe(true);

    gate.resolve(ok({ name: "meme", enabled: false }));
    await waitFor(() => expect(switchOf("meme").getAttribute("aria-checked")).toBe("false"));
    expect(switchOf("meme").disabled).toBe(false);
    expect(switchOf("scheduler").disabled).toBe(false);
  });

  it("says why a change was refused and still reads the status again", async () => {
    const { transport, calls } = routedTransport({
      "ningo.status": [status(UP), status(UP)],
      "services.set": failure("orchestrator", "unknown service"),
    });
    renderCard(transport);
    await screen.findByText("meme");

    fireEvent.click(switchOf("meme"));

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("The orchestrator answered with an error: unknown service");
    await waitFor(() => expect(calls.filter((call) => call.type === "ningo.status")).toHaveLength(2));
    expect(switchOf("meme").getAttribute("aria-checked")).toBe("true");
  });

  it("forgets an old failure when the person refreshes", async () => {
    const { transport } = routedTransport({
      "ningo.status": status(UP),
      "services.set": failure("timeout"),
    });
    renderCard(transport);
    await screen.findByText("meme");
    fireEvent.click(switchOf("meme"));
    await screen.findByRole("alert");
    await screen.findByRole("button", { name: "Refresh" });

    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    await screen.findByText("meme");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("warns that the scheduler switch pauses every job, only when it is there", async () => {
    const withSwitch = routedTransport({ "ningo.status": status(UP) });
    const view = renderCard(withSwitch.transport);
    await screen.findByText("meme");
    screen.getByText("Switching the scheduler off pauses every scheduled job.");
    view.unmount();

    const without = routedTransport({ "ningo.status": status({ scheduler: { status: "ok", detail: "" } }) });
    renderCard(without.transport);
    await screen.findByText("scheduler");
    expect(screen.queryByText(/pauses every scheduled job/)).toBeNull();
    expect(screen.queryAllByRole("switch")).toEqual([]);
  });
});

describe("the scheduled jobs on the health card", () => {
  const JOBS = [
    job("meme.refresh", { lastAt: minutesAgo(120), lastOutcome: "ran" }),
    job("meme.dispatch", { enabled: false, interval: "45m0s", intervalOverride: true, lastError: "channel unreachable" }),
  ];
  const withJobs = (extra: Record<string, unknown> = {}) =>
    routedTransport({ "ningo.status": status(UP, JOBS), "scheduler.jobs.set": ok({ name: "meme.refresh", enabled: false, interval: "6h0m0s", intervalOverride: false }), ...extra });

  it("lists each job with its interval, its last run and its last error", async () => {
    const { transport } = withJobs();
    renderCard(transport);
    await screen.findByText("Scheduled jobs");

    screen.getByText(/every 6h · last run 2 hours ago: ran/);
    screen.getByText(/every 45m · custom · has not run yet/);
    screen.getByText("channel unreachable");
    expect(switchOf("meme.refresh").getAttribute("aria-checked")).toBe("true");
    expect(switchOf("meme.dispatch").getAttribute("aria-checked")).toBe("false");
  });

  it("has no jobs section when the orchestrator reports no jobs", async () => {
    const { transport } = routedTransport({ "ningo.status": status(UP) });
    renderCard(transport);
    await screen.findByText("meme");

    expect(screen.queryByText("Scheduled jobs")).toBeNull();
  });

  it("switches a job with the flag alone, so its interval is left as it is", async () => {
    const { transport, calls } = withJobs();
    renderCard(transport);
    await screen.findByText("Scheduled jobs");

    fireEvent.click(switchOf("meme.dispatch"));

    await waitFor(() => expect(changes(calls)).toHaveLength(1));
    expect(changes(calls)).toEqual([["scheduler.jobs.set", { name: "meme.dispatch", enabled: true }]]);
  });

  it("changes the interval with the interval alone, from the editor's number and unit", async () => {
    const { transport, calls } = withJobs();
    renderCard(transport);
    await screen.findByText("Scheduled jobs");

    expect((screen.getByLabelText("meme.refresh interval") as HTMLInputElement).value).toBe("6");
    expect((screen.getByLabelText("meme.refresh interval unit") as HTMLSelectElement).value).toBe("h");
    fireEvent.change(screen.getByLabelText("meme.refresh interval"), { target: { value: "45" } });
    fireEvent.change(screen.getByLabelText("meme.refresh interval unit"), { target: { value: "m" } });
    const set = screen.getAllByRole("button", { name: "Set" })[0] as HTMLButtonElement;
    fireEvent.click(set);

    await waitFor(() => expect(changes(calls)).toHaveLength(1));
    expect(changes(calls)).toEqual([["scheduler.jobs.set", { name: "meme.refresh", interval: "45m" }]]);
  });

  it("does not offer an interval the orchestrator would refuse", async () => {
    const { transport, calls } = withJobs();
    renderCard(transport);
    await screen.findByText("Scheduled jobs");
    const set = screen.getAllByRole("button", { name: "Set" })[0] as HTMLButtonElement;

    for (const value of ["", "0", "721"]) {
      fireEvent.change(screen.getByLabelText("meme.refresh interval"), { target: { value } });
      expect(set.disabled, `${value}h`).toBe(true);
    }
    fireEvent.change(screen.getByLabelText("meme.refresh interval"), { target: { value: "720" } });
    expect(set.disabled).toBe(false);
    expect(changes(calls)).toEqual([]);
  });

  it("goes back to the default interval only for a job that has a custom one", async () => {
    const { transport, calls } = withJobs();
    renderCard(transport);
    await screen.findByText("Scheduled jobs");

    const reset = screen.getAllByRole("button", { name: "Back to default" });
    expect(reset).toHaveLength(1);
    fireEvent.click(reset[0] as HTMLElement);

    await waitFor(() => expect(changes(calls)).toHaveLength(1));
    expect(changes(calls)).toEqual([["scheduler.jobs.set", { name: "meme.dispatch", interval: "default" }]]);
  });

  it("speaks Portuguese when asked to", async () => {
    const { transport } = withJobs();
    renderCard(transport, "pt");
    await screen.findByText("Jobs agendados");

    expect(screen.getByRole("switch", { name: "chave de meme" }).textContent).toBe("ligado");
    expect(screen.getByRole("switch", { name: "chave de meme.dispatch" }).textContent).toBe("desligado");
    screen.getByText(/a cada 6h · última execução há 2 horas: ran/);
  });
});
