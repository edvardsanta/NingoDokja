import { useCallback, useEffect, useRef, useState } from "react";

import type { Off, PriceProduct, PriceRow, PriceSearch } from "../../shared/replies.js";
import type { Transport } from "../../shared/transport.js";
import { useNumbers, useTimes, useTranslate } from "../i18n/context.js";
import { CardFrame, OffNotice, OpenSystem } from "./card_frame.js";
import {
  VERDICTS,
  cheapestOf,
  leadOf,
  parsePriceSearch,
  signalsOf,
  sparkOf,
  updatedOf,
  verdictCounts,
  verdictOf,
} from "./prices_model.js";
import { useCard, type LoadResult } from "./use_card.js";

const TIMEOUT_MS = 20_000;
// Typing waits this long before it searches, so every key does not cost a request.
const SEARCH_DELAY_MS = 350;

type Words = { query: string; brand: string };

export async function loadPrices(transport: Transport, words: Words): Promise<LoadResult<PriceSearch | Off>> {
  const answer = await transport.request("price.search", { query: words.query, brand: words.brand }, { timeoutMs: TIMEOUT_MS });
  if (!answer.ok) return answer;
  const search = parsePriceSearch(answer.result);
  return search ? { ok: true, data: search } : { ok: false, error: { code: "unexpected", message: "unexpected price reply" } };
}

// The watched prices: what the collectors saw, and whether a price is a real promotion. A view of
// the Library, since prices come in through the feeds like readings do.
export function PricesCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const [typed, setTyped] = useState<Words>({ query: "", brand: "" });
  const [words, setWords] = useState<Words>(typed);
  // The search reads the words through a ref: a new search keeps the results and the fields on
  // screen while it runs (a quiet read), instead of blanking the section under the typing.
  const current = useRef(words);
  current.current = words;
  const load = useCallback(() => loadPrices(transport, current.current), [transport]);
  const { state, reload, poll, refreshing } = useCard(load);
  const first = useRef(true);
  useEffect(() => {
    const timer = setTimeout(() => setWords(typed), SEARCH_DELAY_MS);
    return () => clearTimeout(timer);
  }, [typed]);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    poll();
  }, [words, poll]);

  return (
    <CardFrame title={t("prices_title")} quietTitle fix={<OpenSystem />} state={state} onReload={reload} onRefresh={poll} refreshing={refreshing}>
      {(search) => (
        <>
          <div className="price-filters">
            <label className="price-search">
              {t("prices_product")}
              <input type="search" data-library-search value={typed.query} placeholder={t("prices_product_hint")}
                onChange={(event) => setTyped({ ...typed, query: event.target.value })} />
            </label>
            <label>
              {t("prices_brand")}
              <input type="search" value={typed.brand} placeholder={t("prices_brand_hint")}
                onChange={(event) => setTyped({ ...typed, brand: event.target.value })} />
            </label>
          </div>
          {search.off ? <><OffNotice reason={search.reason} /><OpenSystem /></> : <PriceResults search={search} words={words} />}
        </>
      )}
    </CardFrame>
  );
}

function PriceResults({ search, words }: { search: PriceSearch; words: Words }) {
  const t = useTranslate();
  const [verdict, setVerdict] = useState("");
  const counts = verdictCounts(search.products);
  const shown = verdict ? search.products.filter((product) => leadOf(product).code === verdict) : search.products;
  const searched = words.query.trim() || words.brand.trim();

  return (
    <>
      <div className="price-verdicts" role="group" aria-label={t("prices_verdict_filter")}>
        {VERDICTS.filter((each) => each.code !== "stale").map((each) => (
          <button key={each.code} type="button" className="price-chip" data-tone={each.tone} aria-pressed={verdict === each.code}
            onClick={() => setVerdict(verdict === each.code ? "" : each.code)}>
            <span className="price-dot" aria-hidden="true" />
            {t(each.label)}
            <span className="detail">{counts.get(each.code) ?? 0}</span>
          </button>
        ))}
      </div>
      <p className="detail" aria-live="polite">
        {t("prices_shown", { shown: shown.length, matched: search.matched })}
        {search.matched > search.products.length && ` ${t("prices_more", { count: search.matched - search.products.length })}`}
      </p>
      {shown.length === 0 ? (
        <p className="price-empty">{t(searched || verdict ? "prices_none" : "prices_empty")}</p>
      ) : (
        <ul className="price-grid">
          {shown.map((product) => <PriceCard key={product.key} product={product} />)}
        </ul>
      )}
    </>
  );
}

function sourceLabel(row: PriceRow, t: ReturnType<typeof useTranslate>): string {
  // The merchant is the key the owner's collector chose; the screen names no store of its own.
  if (row.series === "market_min") return t("price_source_market", { merchant: row.merchant });
  if (row.kind === "unknown") return row.merchant;
  return t(row.kind === "pix" ? "price_source_pix" : "price_source_card", { merchant: row.merchant });
}

function PriceCard({ product }: { product: PriceProduct }) {
  const t = useTranslate();
  const { money } = useNumbers();
  const { day } = useTimes();
  const lead = leadOf(product);
  const cheapest = cheapestOf(product);
  const signals = signalsOf(product);
  const spark = sparkOf(product.market);
  const updated = updatedOf(product);

  return (
    <li className="price-card" data-tone={lead.tone}>
      <div className="price-card-head">
        <h3>{product.title}</h3>
        <span className="price-badge">{t(lead.label)}</span>
      </div>
      {cheapest && (
        <p className="price-best">
          <strong>{money(cheapest.latestCents)}</strong>
          <span className="detail">{t("prices_cheapest", { source: sourceLabel(cheapest, t) })}</span>
        </p>
      )}
      {signals.length > 0 && <p className="price-signals">{signals.map((signal) => t(signal)).join(" · ")}</p>}
      {spark ? (
        <figure className="price-chart">
          <figcaption className="detail">
            <span>{t("prices_market_year")}</span>
            <span>{t("prices_range", { min: money(spark.min), max: money(spark.max) })}</span>
          </figcaption>
          <svg viewBox="0 0 320 64" preserveAspectRatio="none" role="img"
            aria-label={t("prices_chart_label", { min: money(spark.min), max: money(spark.max) })}>
            <polyline points={spark.points} fill="none" vectorEffect="non-scaling-stroke" />
            <circle cx={spark.lastX} cy={spark.lastY} r="3.5" />
          </svg>
        </figure>
      ) : (
        <p className="price-no-chart detail">{t("prices_no_market")}</p>
      )}
      <ul className="price-rows">
        {product.rows.map((row) => {
          const verdict = verdictOf(row.verdict);
          const details = [
            row.median30Cents > 0 ? t("prices_median", { price: money(row.median30Cents) }) : "",
            row.lowestCents > 0 ? t("prices_lowest", { price: money(row.lowestCents) }) : "",
          ].filter(Boolean);
          return (
            <li key={`${row.merchant}:${row.series}:${row.kind}`}>
              <span>{sourceLabel(row, t)}</span>
              <strong>{money(row.latestCents)}</strong>
              <span className="detail">{details.length ? details.join(" · ") : t("prices_counting")}</span>
              <span className="price-row-verdict" data-tone={verdict.tone}>
                <span className="price-dot" aria-hidden="true" />
                {t(verdict.label)}
              </span>
            </li>
          );
        })}
      </ul>
      {updated && <p className="detail">{t("prices_updated", { day: day(`${updated}T12:00:00`) })}</p>}
    </li>
  );
}
