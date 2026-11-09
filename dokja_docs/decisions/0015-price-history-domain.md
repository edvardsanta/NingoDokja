# ADR 0015: a price domain that owns watched-product price history

Date: 2026-11-08. Status: proposed by the owner's request; the domain contract,
rules, reference repository, orchestrator intake and PostgreSQL adapter are
implemented. Commands and alerts are pending.
Related: [ADR 0002](./0002-plugin-lifecycle.md), [ADR 0003](./0003-state-ownership.md),
[ADR 0014](./0014-application-postgres-adoption.md).

## Context

The owner wants to know whether a promotion on the storage devices they watch
is real: whether today's price is under what the product usually costs, or a
discount against a "from" price that was never charged, or a return to normal
after a markup. That needs a daily history per product, kept over months.

Three facts shape the design:

- Collecting from stores and aggregators is scraping. The repository is public
  and ships mechanisms only, so collectors cannot be committed. They are owner
  plugins in the Feeds plugin directory, outside git.
- Feeds keeps only the last successful run of each plugin in memory, at most 200
  items and 1 MiB per run. It can carry today's observations and a bounded
  backfill, but it cannot hold a history.
- ADR 0003 authorizes no new durable state without a decision, and services are
  stateless. A history therefore needs an explicit owner.

## Decision

1. **A new domain, `dokja_domain/dokja_price`, owns price history and its
   interpretation.** It knows no store, site or URL. It reads one contract and
   decides what a price means.
2. **Collectors speak `price.observation.v1`, carried in a Feeds item `body`,
   and mark the item with `kind: "price"`.** The domain refuses any other shape
   it is handed and leaves non-price items alone. The kind keeps observations
   out of the digest and out of conversation references; the contract itself is
   the opt-in, so no list of sources is configured:

   ```json
   {"contract": "price.observation.v1", "product": "ssd-example-1tb",
    "title": "SSD Example 1TB", "merchant": "store-a", "merchant_name": "Store A",
    "series": "merchant", "price_kind": "pix", "currency": "BRL",
    "points": [{"on": "2026-11-08", "price": 459.90, "list_price": 899.00}]}
   ```

   - `product` and `merchant` are owner-chosen keys (lowercase letters, digits,
     `.`, `_`, `:` and `-`, at most 80).
   - `series` is either `merchant` (one merchant's own price) or `market_min`
     (an aggregator's daily lowest price across merchants; an optional per-point
     `seller` is informative only).
   - `price_kind` is `pix`, `card` or `unknown`. Cash and card prices of one offer
     are separate histories.
   - `list_price` is optional. It is the struck-through "from" price the merchant
     advertises for the offer. It is never the price of the other payment kind:
     a card price shown next to a lower cash price is a separate `card` history.
     Writing it as the cash point's list price would make every cash price look
     like a discount and produce `fake_discount`. Omit it when the store shows
     none.
   - `points` holds 1 to 400 days. A daily snapshot has one point; a backfill of
     an aggregator's year of history has about 366 (roughly 20 KB), so a backfill
     of many products is spread across several runs to stay within Feeds limits.
   - Amounts are decimals in reais. Up to 15 decimals are accepted, because
     sources computing in floating point print noise, and they are rounded half
     up to the cent. Unknown fields, other currencies, repeated or future dates
     are refused.
3. **One point per history and day; writes are idempotent.** A history is
   (product, merchant, series, price kind). Replaying an observation writes
   nothing, and a later observation of the same day replaces the earlier one.
   Histories of different series or price kinds are never compared.
4. **The domain returns codes, not prose.** An assessment carries a verdict
   (`insufficient_history`, `stale`, `real_discount`, `inflated_before_sale`,
   `fake_discount`, `no_discount`), signals (`all_time_low`,
   `list_above_observed_max`, `raised_before_drop`) and the medians it used.
   Interfaces translate them through their locales.
5. **Storage sits behind the domain's `Repository` port.** The in-memory
   repository is the reference behavior and the test store. Following ADR 0014,
   the durable adapter is PostgreSQL, confirmed by the owner on 2026-11-08 as an
   exception to ADR 0014's 2026-10-26 pause on new storage work. It gets a schema owned by this domain,
   selected explicitly, with no SQLite fallback. This amends ADR 0003 for this
   one owner only. It authorizes no shared workspace database and does not move
   any existing state.

## Rules (first version)

Thresholds are constants in `assess.go`, in basis points:

- At least 7 earlier days in the last 30 are needed for a verdict.
- `real_discount`: at least 5% under the 30-day median, and also under the
  90-day median when there is one.
- `inflated_before_sale`: the peak of the last 14 days exceeded the usual price
  (the median of days 15 to 74 before) by 10%, and the latest price is lower
  than that peak but within 5% of the usual price.
- `fake_discount`: the merchant advertises at least 10% off its list price, and
  the history shows no real discount.
- `stale`: the latest point is more than two days older than the UTC day being
  judged. Aggregators publish only closed days, so their latest point is
  yesterday in Brazil, two UTC days back late in the evening.

## Orchestrator intake (implemented)

- **Scheduler.** The `price.intake` job emits `price.intake.run` every 15
  minutes by default, matching a Feeds collector's default interval. Feeds
  keeps only a collector's last run, so reading less often would lose the
  chunks of a multi-run backfill. Like every job, it can be paused or given
  another interval through the controls; no variable was added.
- **Intake task.** A typed task reads `feeds.items` without refreshing and
  hands every item to `record-price-observations`. It skips the run while
  Feeds is switched off or another run is going.
- **Route.** `price.assess` routes to `assess-price`. It works while Feeds is
  off, because it reads only the history.
- **Digest.** It drops items of kind `price` in the one place it reads Feeds
  items, so observations appear neither in the digest nor in chat references.

## PostgreSQL adapter (implemented)

- `dokja_orch/internal/infrastructure/price_history` implements the port in a
  `dokja_price` schema: `schema_version`, `products` and `points`, keyed by
  product, merchant, series, price kind and day. An upsert rewrites a point only
  when it changed, so it counts writes the way the memory repository does.
- `DOKJA_PRICE_POSTGRES_DSN` selects it explicitly. The explicit
  `price-history-init` command creates the namespace; startup only opens it.
  Unset, or when the database cannot be used, history stays in memory and a
  sanitized message says so. The DSN is never logged.
- An integration test, built with the `integration` tag against a disposable
  `price_test` database, runs the same writes against both repositories and
  compares their answers.
- Backup, rollback and configuration are in the production runbook.

## Pending work (stacked follow-ups)

- **Collection runs with the stack.** Observations are recorded only while the
  owner's stack is up (`make start`); there is no separate host capture, so days
  the stack is down have no merchant points. The aggregator's rolling history
  can backfill the market minimum for those days.
- **Commands and alerts** in Desktop, CLI or Discord. These need the owner's
  approval of the command surface before they are added.

## Compatibility

PR 1 adds a Go module and its CI step only. It changes no event envelope,
route, service contract, command, schema, environment variable or Compose file.
Collectors and their target addresses stay outside the repository. Tests and
documentation use invented products and merchants.
