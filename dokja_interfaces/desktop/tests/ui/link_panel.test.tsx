import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { HealthCard } from "../../src/renderer/cards/health.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import { StatusProvider } from "../../src/renderer/status_context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { Transport } from "../../src/shared/transport.js";
import { failure, ok, routedTransport } from "./support.js";

const IMAGE = "data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=";

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <StatusProvider transport={transport}>
        <HealthCard transport={transport} />
      </StatusProvider>
    </I18nProvider>,
  );
}

const status = (state: string, extra: Record<string, unknown> = {}) =>
  ok({
    status: "degraded",
    services: {
      meme: { status: "ok", detail: "", enabled: true },
      whatsapp: { status: state === "connected" ? "ok" : "error", detail: "", enabled: true, kind: "interface", state, ...extra },
    },
  });

const waiting = ok({ state: "waiting_for_link", image: IMAGE });
const calls = (list: Array<{ type: string }>, type: string) => list.filter((call) => call.type === type);

async function openPanel() {
  const button = await screen.findByRole("button", { name: /^(Link WhatsApp|Vincular WhatsApp)$/ });
  fireEvent.click(button);
  return button;
}

describe("linking WhatsApp from the health card", () => {
  it("offers the link once, where WhatsApp needs attention, and shows nothing until it is opened", async () => {
    const { transport, calls: seen } = routedTransport({ "ningo.status": status("waiting_for_link") });
    renderCard(transport);
    await screen.findByText("WhatsApp", { selector: ".services .service-name" });

    const buttons = screen.getAllByRole("button", { name: "Link WhatsApp" });
    expect(buttons).toHaveLength(1);
    expect(buttons[0]?.closest(".attention")).not.toBeNull();
    screen.getByText("Linking is offered above, under Needs attention.");
    expect(screen.queryByRole("heading", { name: "Link WhatsApp" })).toBeNull();
    expect(calls(seen, "communications.link.status")).toHaveLength(0);
  });

  it("asks for the state of the fixed interface and draws the picture of the code", async () => {
    const { transport, calls: seen } = routedTransport({
      "ningo.status": status("waiting_for_link"),
      "communications.link.status": waiting,
    });
    renderCard(transport);
    await openPanel();

    const image = (await screen.findByRole("img", { name: "Code to scan with the phone" })) as HTMLImageElement;

    expect(image.getAttribute("src")).toBe(IMAGE);
    expect(screen.getByText(/Linked devices > Link a device/)).toBeTruthy();
    expect(calls(seen, "communications.link.status")[0]).toMatchObject({ payload: { interface: "whatsapp" } });
  });

  it("says it in Portuguese too", async () => {
    const { transport } = routedTransport({
      "ningo.status": status("waiting_for_link"),
      "communications.link.status": waiting,
    });
    renderCard(transport, "pt");
    await openPanel();

    expect(await screen.findByRole("heading", { name: "Vincular o WhatsApp" })).toBeTruthy();
    expect(screen.getByRole("img", { name: "Código para escanear com o celular" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Desvincular e recomeçar" })).toBeTruthy();
  });

  it("refuses to show anything that is not the picture the shell draws", async () => {
    const { transport } = routedTransport({
      "ningo.status": status("waiting_for_link"),
      "communications.link.status": ok({ state: "waiting_for_link", image: "https://example.com/code.png" }),
    });
    renderCard(transport);
    await openPanel();

    await screen.findByText("Waiting for a code…");
    expect(screen.queryByRole("img", { name: "Code to scan with the phone" })).toBeNull();
  });

  it("asks for a new code with a change, then reads the status again", async () => {
    const { transport, calls: seen } = routedTransport({
      "ningo.status": status("waiting_for_link"),
      "communications.link.status": waiting,
      "communications.link.start": ok({ state: "connecting" }),
    });
    renderCard(transport);
    await openPanel();
    await screen.findByRole("img", { name: "Code to scan with the phone" });

    fireEvent.click(screen.getByRole("button", { name: "New code" }));

    await waitFor(() => expect(calls(seen, "communications.link.start")).toHaveLength(1));
    expect(calls(seen, "communications.link.start")[0]).toMatchObject({ payload: { interface: "whatsapp" } });
    await waitFor(() => expect(calls(seen, "ningo.status").length).toBeGreaterThan(1));
  });

  it("unlinks and starts over, and a no to the window is not shown as a failure", async () => {
    const { transport, calls: seen } = routedTransport({
      "ningo.status": status("connected"),
      "communications.link.status": ok({ state: "connected" }),
      "communications.link.reset": [failure("cancelled", "the person did not confirm"), ok({ state: "waiting_for_link", image: IMAGE })],
    });
    renderCard(transport);
    await openPanel();
    await screen.findByText("Linked and connected.");

    fireEvent.click(screen.getByRole("button", { name: "Unlink and start over" }));
    await waitFor(() => expect(calls(seen, "communications.link.reset")).toHaveLength(1));
    expect(screen.queryByRole("alert")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Unlink and start over" }));
    await waitFor(() => expect(calls(seen, "communications.link.reset")).toHaveLength(2));
    await screen.findByRole("img", { name: "Code to scan with the phone" });
  });

  it("shows why it could not ask, and offers no new code once the account is connected", async () => {
    const { transport } = routedTransport({
      "ningo.status": status("connected"),
      "communications.link.status": failure("orchestrator", "the whatsapp interface did not answer the link request"),
    });
    renderCard(transport);
    await openPanel();

    const alert = await screen.findByRole("alert");
    expect(within(alert).getByText(/did not answer the link request/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New code" })).toBeNull();
  });
});

describe("the link panel while it stays open", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("reads the state again every couple of seconds, tells the card when it changes, and stops when closed", async () => {
    const { transport, calls: seen } = routedTransport({
      "ningo.status": status("waiting_for_link"),
      "communications.link.status": [waiting, waiting, ok({ state: "connected" })],
    });
    renderCard(transport);
    await openPanel();
    await screen.findByRole("img", { name: "Code to scan with the phone" });
    const statusReads = () => calls(seen, "ningo.status").length;
    const before = statusReads();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });
    expect(calls(seen, "communications.link.status")).toHaveLength(2);
    expect(statusReads()).toBe(before);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });
    await screen.findByText("Linked and connected.");
    await waitFor(() => expect(statusReads()).toBeGreaterThan(before));

    // The card read its status again, so the button is a new element, and the panel is still open.
    fireEvent.click(await screen.findByRole("button", { name: "Close" }));
    const reads = calls(seen, "communications.link.status").length;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });
    expect(calls(seen, "communications.link.status")).toHaveLength(reads);
  });
});

it("keeps an unconfigured WhatsApp visible without pretending that it can link",async()=>{
 const {transport,calls:seen}=routedTransport({"ningo.status":status("not_configured",{status:"unchecked",configured:false})});
 renderCard(transport);
 await screen.findByText("WhatsApp", { selector: ".services .service-name" });
 screen.getByText("Not configured", { selector: ".services .service-status" });screen.getByText(/Configure the local WhatsApp gateway/);
 expect(screen.queryByRole("button",{name:"Link WhatsApp"})).toBeNull();
 expect((screen.getByRole("switch",{name:"WhatsApp switch"}) as HTMLButtonElement).disabled).toBe(true);
 expect(calls(seen,"communications.link.status")).toHaveLength(0);
});

it("says plainly that linking needs the messages set up, instead of the raw error", async () => {
  const { transport } = routedTransport({
    "ningo.status": status("waiting_for_link"),
    "communications.link.status": failure("orchestrator", "Communications access is not configured."),
  });
  renderCard(transport);
  await openPanel();
  await screen.findByText("Linking needs the messages to be set up first.");
  screen.getByText(/DOKJA_COMMUNICATIONS_TOKEN/);
  expect(screen.queryByRole("alert")).toBeNull();
});

it("keeps the link on the row, not under Needs attention, once WhatsApp is connected", async () => {
  const { transport } = routedTransport({ "ningo.status": status("connected") });
  renderCard(transport);
  const button = await screen.findByRole("button", { name: "Link WhatsApp" });
  expect(button.closest(".attention")).toBeNull();
});
