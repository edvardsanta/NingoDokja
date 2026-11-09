import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PricesCard } from "../../src/renderer/cards/prices.js";
import { WorkspacePanel } from "../../src/renderer/cards/workspace_panel.js";
import { I18nProvider } from "../../src/renderer/i18n/context.js";
import type { Locale } from "../../src/shared/locale.js";
import type { PriceProduct, PriceSearch } from "../../src/shared/replies.js";
import type { Transport } from "../../src/shared/transport.js";
import { OFF, ok, routedTransport } from "./support.js";

function renderCard(transport: Transport, locale: Locale = "en") {
  return render(
    <I18nProvider locale={locale}>
      <WorkspacePanel trail={["Library"]}>
        <PricesCard transport={transport} />
      </WorkspacePanel>
    </I18nProvider>,
  );
}

const GPU: PriceProduct = {
  key: "gpu-example", title: "GPU Example 12GB",
  rows: [
    { merchant: "store-a", series: "merchant", kind: "pix", verdict: "insufficient_history", signals: [], latestOn: "2026-11-08", latestCents: 618_599, median30Cents: 0, lowestCents: 0 },
    { merchant: "aggregator", series: "market_min", kind: "unknown", verdict: "inflated_before_sale", signals: ["raised_before_drop"], latestOn: "2026-11-07", latestCents: 647_058, median30Cents: 1_096_137, lowestCents: 399_999 },
  ],
  market: [{ on: "2026-11-05", cents: 1_096_137 }, { on: "2026-11-06", cents: 1_096_137 }, { on: "2026-11-07", cents: 647_058 }],
};
const CPU: PriceProduct = {
  key: "cpu-example", title: "CPU Example 8C",
  rows: [{ merchant: "aggregator", series: "market_min", kind: "unknown", verdict: "real_discount", signals: ["all_time_low"], latestOn: "2026-11-07", latestCents: 214_900, median30Cents: 238_999, lowestCents: 214_900 }],
  market: [],
};
const search = (products: PriceProduct[], matched = products.length): PriceSearch => ({ products, matched });

describe("the watched prices", () => {
  it("shows one card per product, toned by its leading verdict, with its sources and chart", async () => {
    const { calls, transport } = routedTransport({ "price.search": ok(search([GPU, CPU])) });
    renderCard(transport);
    const gpu = (await screen.findByRole("heading", { name: "GPU Example 12GB" })).closest("li")!;
    expect(gpu.getAttribute("data-tone")).toBe("warning");
    expect(within(gpu).getByText("Inflated before the sale", { selector: ".price-badge" })).toBeTruthy();
    expect(within(gpu).getByText("rose before dropping")).toBeTruthy();
    expect(within(gpu).getByText("store-a · PIX")).toBeTruthy();
    expect(within(gpu).getByText("market low · aggregator")).toBeTruthy();
    expect(within(gpu).getByRole("img", { name: /Market low over the last 12 months/ })).toBeTruthy();
    // the cheapest offer now leads the card
    expect(within(gpu).getByText("lowest now · store-a · PIX")).toBeTruthy();

    const cpu = screen.getByRole("heading", { name: "CPU Example 8C" }).closest("li")!;
    expect(cpu.getAttribute("data-tone")).toBe("good");
    expect(within(cpu).getByText(/No market history/)).toBeTruthy();
    expect(calls[0]).toMatchObject({ type: "price.search", payload: { query: "", brand: "" } });
  });

  it("filters by verdict on the screen and counts each verdict", async () => {
    const { transport } = routedTransport({ "price.search": ok(search([GPU, CPU])) });
    renderCard(transport);
    await screen.findByRole("heading", { name: "GPU Example 12GB" });
    const real = screen.getByRole("button", { name: /Real discount\s*1/ });
    fireEvent.click(real);
    expect(real.getAttribute("aria-pressed")).toBe("true");
    expect(screen.queryByRole("heading", { name: "GPU Example 12GB" })).toBeNull();
    expect(screen.getByRole("heading", { name: "CPU Example 8C" })).toBeTruthy();
    fireEvent.click(real);
    expect(screen.getByRole("heading", { name: "GPU Example 12GB" })).toBeTruthy();
  });

  it("searches the words typed, keeping the fields on screen while it reads", async () => {
    const { calls, transport } = routedTransport({ "price.search": [ok(search([GPU, CPU])), ok(search([CPU]))] });
    renderCard(transport);
    await screen.findByRole("heading", { name: "GPU Example 12GB" });
    fireEvent.change(screen.getByLabelText("Brand"), { target: { value: "amd" } });
    await waitFor(() => expect(calls.length).toBe(2));
    expect(calls[1]).toMatchObject({ type: "price.search", payload: { query: "", brand: "amd" } });
    await waitFor(() => expect(screen.queryByRole("heading", { name: "GPU Example 12GB" })).toBeNull());
    expect((screen.getByLabelText("Brand") as HTMLInputElement).value).toBe("amd");
  });

  it("says when nothing matches, when products did not fit and when prices are switched off", async () => {
    const { transport } = routedTransport({ "price.search": ok(search([CPU], 14)) });
    renderCard(transport, "pt");
    expect(await screen.findByText(/Mais 13 não couberam/)).toBeTruthy();

    const empty = routedTransport({ "price.search": ok(search([])) });
    renderCard(empty.transport);
    expect(await screen.findByText(/No prices yet/)).toBeTruthy();

    const off = routedTransport({ "price.search": ok(OFF) });
    renderCard(off.transport);
    expect(await screen.findByText(/paused by the operator/)).toBeTruthy();
  });
});
