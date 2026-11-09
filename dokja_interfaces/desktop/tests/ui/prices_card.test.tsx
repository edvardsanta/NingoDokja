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
    { merchant: "store-a", series: "merchant", kind: "pix", verdict: "insufficient_history", signals: [], latestOn: "2026-11-08", latestCents: 618_599, median30Cents: 0, lowestCents: 0, offer: "" },
    { merchant: "aggregator", series: "market_min", kind: "unknown", verdict: "inflated_before_sale", signals: ["raised_before_drop"], latestOn: "2026-11-07", latestCents: 647_058, median30Cents: 1_096_137, lowestCents: 399_999, offer: "" },
  ],
  market: [{ on: "2026-11-05", cents: 1_096_137 }, { on: "2026-11-06", cents: 1_096_137 }, { on: "2026-11-07", cents: 647_058 }],
};
const CPU: PriceProduct = {
  key: "cpu-example", title: "CPU Example 8C",
  rows: [{ merchant: "aggregator", series: "market_min", kind: "unknown", verdict: "real_discount", signals: ["all_time_low"], latestOn: "2026-11-07", latestCents: 214_900, median30Cents: 238_999, lowestCents: 214_900, offer: "" }],
  market: [],
};
const search = (products: PriceProduct[], matched = products.length): PriceSearch => ({ products, matched, merchants: ["aggregator", "store-a"] });

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
    expect(calls.find((call) => call.type === "price.search")).toMatchObject({ payload: { query: "", brand: "", merchant: "" } });
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
    fireEvent.change(screen.getByLabelText("Brand"), { target: { value: "brandz" } });
    const searches = () => calls.filter((call) => call.type === "price.search");
    await waitFor(() => expect(searches().length).toBe(2));
    expect(searches()[1]).toMatchObject({ type: "price.search", payload: { query: "", brand: "brandz" } });
    await waitFor(() => expect(screen.queryByRole("heading", { name: "GPU Example 12GB" })).toBeNull());
    expect((screen.getByLabelText("Brand") as HTMLInputElement).value).toBe("brandz");
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

describe("watching searches, picking a store and opening a store page", () => {
  const withOffer: PriceProduct = { ...CPU, rows: [{ ...CPU.rows[0]!, offer: "0123456789abcdef" }] };

  it("searches one store, chosen from the stores already seen", async () => {
    const { calls, transport } = routedTransport({ "price.search": ok(search([GPU, CPU])), "price.watches": ok({ watches: [], limit: 50 }) });
    renderCard(transport);
    await screen.findByRole("heading", { name: "GPU Example 12GB" });
    const store = screen.getByLabelText("Store") as HTMLSelectElement;
    expect([...store.options].map((option) => option.value)).toEqual(["", "aggregator", "store-a"]);
    fireEvent.change(store, { target: { value: "store-a" } });
    await waitFor(() => expect(calls.filter((call) => call.type === "price.search").length).toBe(2));
    expect(calls.filter((call) => call.type === "price.search")[1]).toMatchObject({ payload: { merchant: "store-a" } });
  });

  it("watches a search only on a press, says so and lists it; typing alone watches nothing", async () => {
    const watched = { ...search([CPU]), watch: { category: "ssd sata", brand: "brandx", status: "added" as const } };
    const { calls, transport } = routedTransport({
      "price.search": [ok(search([GPU, CPU])), ok(search([CPU])), ok(watched), ok(search([CPU]))],
      "price.watches": [ok({ watches: [], limit: 50 }), ok({ watches: [{ category: "ssd sata", brand: "brandx" }], limit: 50 })],
    });
    renderCard(transport, "pt");
    await screen.findByRole("heading", { name: "GPU Example 12GB" });
    const watch = screen.getByRole("button", { name: "Vigiar esta busca" });
    expect((watch as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Produto"), { target: { value: "ssd sata" } });
    fireEvent.change(screen.getByLabelText("Marca"), { target: { value: "brandx" } });
    await waitFor(() => expect(calls.filter((call) => call.type === "price.search").length).toBeGreaterThan(1));
    expect(calls.some((call) => call.type === "price.search" && call.payload?.watch === true)).toBe(false);

    fireEvent.click(watch);
    expect(await screen.findByText("Passei a vigiar ssd sata · brandx. Os produtos chegam em instantes.")).toBeTruthy();
    const asked = calls.find((call) => call.type === "price.search" && call.payload?.watch === true);
    expect(asked?.payload).toMatchObject({ query: "ssd sata", brand: "brandx", watch: true });
    await waitFor(() => expect(screen.getByText("Buscas vigiadas (1)")).toBeTruthy());
  });

  it("removes a watched search", async () => {
    const { calls, transport } = routedTransport({
      "price.search": ok(search([CPU])),
      "price.watches": [ok({ watches: [{ category: "hd", brand: "brandv" }], limit: 50 }), ok({ watches: [], limit: 50 })],
      "price.unwatch": ok({ removed: true }),
    });
    renderCard(transport);
    fireEvent.click(await screen.findByText("Watched searches (1)"));
    fireEvent.click(screen.getByRole("button", { name: "Stop watching hd · brandv" }));
    await waitFor(() => expect(screen.getByText("Watched searches (0)")).toBeTruthy());
    expect(calls.find((call) => call.type === "price.unwatch")?.payload).toEqual({ category: "hd", brand: "brandv" });
  });

  it("asks the shell to open a store page by its token, and offers nothing without one", async () => {
    const routed = routedTransport({ "price.search": ok(search([withOffer, GPU])), "price.watches": ok({ watches: [], limit: 50 }) });
    const opened: string[] = [];
    const transport: Transport = { ...routed.transport, openOffer: async (offer) => { opened.push(offer); return ok({ opened: true }); } };
    renderCard(transport);
    const open = await screen.findByRole("button", { name: "Open market low · aggregator at the store" });
    fireEvent.click(open);
    expect(opened).toEqual(["0123456789abcdef"]);
    const gpu = screen.getByRole("heading", { name: "GPU Example 12GB" }).closest("li")!;
    expect(within(gpu).queryByRole("button", { name: /at the store/ })).toBeNull();
  });
});
