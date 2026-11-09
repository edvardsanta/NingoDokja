import type { Off, PricePoint, PriceProduct, PriceRow, PriceSearch, PriceWatches, PriceWatchOutcome } from "../../shared/replies.js";
import type { MessageId } from "../i18n/i18n.js";

// The verdicts the Price domain returns, in the order that decides which one speaks for a product:
// a warning first, then a real discount, then the rest. Each has its words and a tone the
// stylesheet colours; an unknown code ranks last and reads as "no verdict".
export const VERDICTS = [
  { code: "fake_discount", label: "price_verdict_fake_discount", tone: "problem" },
  { code: "inflated_before_sale", label: "price_verdict_inflated_before_sale", tone: "warning" },
  { code: "real_discount", label: "price_verdict_real_discount", tone: "good" },
  { code: "no_discount", label: "price_verdict_no_discount", tone: "plain" },
  { code: "stale", label: "price_verdict_stale", tone: "quiet" },
  { code: "insufficient_history", label: "price_verdict_insufficient_history", tone: "quiet" },
] as const satisfies ReadonlyArray<{ code: string; label: MessageId; tone: string }>;

export type VerdictTone = (typeof VERDICTS)[number]["tone"] | "quiet";
export type Verdict = { code: string; label: MessageId; tone: VerdictTone; rank: number };

const UNKNOWN: Verdict = { code: "", label: "price_verdict_unknown", tone: "quiet", rank: VERDICTS.length };

export function verdictOf(code: string): Verdict {
  const index = VERDICTS.findIndex((verdict) => verdict.code === code);
  const found = VERDICTS[index];
  return found ? { code: found.code, label: found.label, tone: found.tone, rank: index } : UNKNOWN;
}

const SIGNALS: Record<string, MessageId> = {
  all_time_low: "price_signal_all_time_low",
  raised_before_drop: "price_signal_raised_before_drop",
  list_above_observed_max: "price_signal_list_above_observed_max",
};

// The words for the signals a product's histories carry, each once, in the order first seen.
export function signalsOf(product: PriceProduct): MessageId[] {
  const seen = new Set<string>();
  const out: MessageId[] = [];
  for (const row of product.rows) {
    for (const signal of row.signals) {
      const label = SIGNALS[signal];
      if (label && !seen.has(signal)) {
        seen.add(signal);
        out.push(label);
      }
    }
  }
  return out;
}

// The verdict that speaks for a product: its most urgent history's.
export function leadOf(product: PriceProduct): Verdict {
  return product.rows.map((row) => verdictOf(row.verdict)).sort((a, b) => a.rank - b.rank)[0] ?? UNKNOWN;
}

// The lowest price on offer now, among every history of the product.
export function cheapestOf(product: PriceProduct): PriceRow | undefined {
  return [...product.rows].sort((a, b) => a.latestCents - b.latestCents)[0];
}

// The newest day any history of the product was seen.
export function updatedOf(product: PriceProduct): string {
  return product.rows.map((row) => row.latestOn).filter(Boolean).sort().at(-1) ?? "";
}

// How the products are split by their leading verdict, for the filter.
export function verdictCounts(products: PriceProduct[]): Map<string, number> {
  const counts = new Map<string, number>();
  for (const product of products) {
    const code = leadOf(product).code;
    counts.set(code, (counts.get(code) ?? 0) + 1);
  }
  return counts;
}

export type Spark = { points: string; lastX: number; lastY: number; min: number; max: number };

// The market-low line in a box of `width`×`height`, oldest on the left. Fewer than two points draw
// nothing: one day is not a trend.
export function sparkOf(market: PricePoint[], width = 320, height = 64, pad = 4): Spark | undefined {
  if (market.length < 2) return undefined;
  const values = market.map((point) => point.cents);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = Math.max(max - min, 1);
  const round = (value: number) => Math.round(value * 10) / 10;
  const coords = values.map((value, index): [number, number] => [
    round((index / (values.length - 1)) * width),
    round(height - pad - ((value - min) / span) * (height - 2 * pad)),
  ]);
  const [lastX, lastY] = coords[coords.length - 1] ?? [0, 0];
  return { points: coords.map((pair) => pair.join(",")).join(" "), lastX, lastY, min, max };
}

const PRICE_DAY = /^\d{4}-\d{2}-\d{2}$/;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

// Checks what the shell handed over; it is already reduced, so anything off-shape means a bug.
export function parsePriceSearch(value: unknown): PriceSearch | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: typeof value.reason === "string" ? value.reason : "" };
  if (!Array.isArray(value.products)) return undefined;
  const products: PriceProduct[] = value.products.filter(isRecord).map((product) => ({
    key: String(product.key ?? ""),
    title: String(product.title ?? product.key ?? ""),
    rows: (Array.isArray(product.rows) ? product.rows : []).filter(isRecord).map((row): PriceRow => ({
      merchant: String(row.merchant ?? ""),
      series: row.series === "market_min" ? "market_min" : "merchant",
      kind: row.kind === "pix" || row.kind === "card" ? row.kind : "unknown",
      verdict: String(row.verdict ?? ""),
      signals: Array.isArray(row.signals) ? row.signals.map(String) : [],
      latestOn: typeof row.latestOn === "string" && PRICE_DAY.test(row.latestOn) ? row.latestOn : "",
      latestCents: Number(row.latestCents) || 0,
      median30Cents: Number(row.median30Cents) || 0,
      lowestCents: Number(row.lowestCents) || 0,
      offer: typeof row.offer === "string" && /^[0-9a-f]{16}$/.test(row.offer) ? row.offer : "",
    })),
    market: (Array.isArray(product.market) ? product.market : []).filter(isRecord)
      .map((point) => ({ on: String(point.on ?? ""), cents: Number(point.cents) || 0 }))
      .filter((point) => PRICE_DAY.test(point.on) && point.cents > 0),
  })).filter((product) => product.key !== "");
  const matched = typeof value.matched === "number" ? Math.max(value.matched, products.length) : products.length;
  const merchants = Array.isArray(value.merchants) ? value.merchants.filter((merchant): merchant is string => typeof merchant === "string" && merchant !== "") : [];
  const watch = isRecord(value.watch) && typeof value.watch.category === "string" && WATCH_STATUSES.has(String(value.watch.status))
    ? { watch: { category: value.watch.category, brand: typeof value.watch.brand === "string" ? value.watch.brand : "", status: value.watch.status as PriceWatchOutcome["status"] } }
    : {};
  return { products, matched, merchants, ...watch };
}

const WATCH_STATUSES = new Set(["added", "watched", "full", "not_owner"]);

// Checks the watched searches the shell handed over.
export function parsePriceWatches(value: unknown): PriceWatches | Off | undefined {
  if (!isRecord(value)) return undefined;
  if (value.off === true) return { off: true, reason: typeof value.reason === "string" ? value.reason : "" };
  if (!Array.isArray(value.watches)) return undefined;
  const watches = value.watches.filter(isRecord)
    .map((watch) => ({ category: String(watch.category ?? ""), brand: String(watch.brand ?? "") }))
    .filter((watch) => watch.category !== "");
  return { watches, limit: typeof value.limit === "number" ? value.limit : 0 };
}

// "ssd sata · brandx", as a watched search reads.
export function watchLabel(watch: { category: string; brand: string }): string {
  return [watch.category, watch.brand].filter(Boolean).join(" · ");
}
