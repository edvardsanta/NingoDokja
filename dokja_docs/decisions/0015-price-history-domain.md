# ADR 0015: a price domain that owns watched-product price history

Date: 2026-11-08. Status: proposed by the owner's request; the domain contract,
rules, reference repository, orchestrator intake and PostgreSQL adapter are
implemented, and Discord's `/preco` searches them. Alerts are pending.
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

- **Scheduler.** The `price.intake` job emits `price.intake.run` every 6 hours
  by default: each run queries the stores for every watched search. Like every
  job, it can be switched off or given another interval through the controls;
  no variable was added.
- **Intake task (collection by watched search).** A typed task in three steps:
  - plan: the domain's watched searches, or the one the owner just added; up
    to 10 products that lack a market history, for the collector to look up;
    and up to 40 known market pages, the least recently seen first, for it to
    read again for today's store offers and market history;
  - collect: `feeds.invoke` with `price.resolve.v1`, action `search`, input
    `{searches: [{category, brand}], limit: 30, market: [{product, title,
    part_number, page?}]}`; the plugin answers `{observations: [...]}`. A
    market history carries its market page as `url`; the domain keeps it per
    history and hands it back as `page`, so a page is found once and then
    refreshed. Store offers read from that page become one `merchant` history
    per store;
  - record: each observation goes to `record-price-observations` as an item
    body, the one shape the domain validates.

  A scheduled run collects every watched search; a search the owner just added
  is collected at once, on its own. Asked while a run is going, both wait and
  run next. The run is skipped while Feeds is switched off, and fails with
  `no_watches` when nothing is watched. This replaces reading `feeds.items`:
  the scheduled collector (`source.collect.v1`) is retired for prices, so only
  one path writes them.
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

## Discord search (implemented)

The owner asked for `/preco produto:<words>` and `/preco marca:<brand>`.
`price.search` (action `search-prices`) takes optional `product` (exact key),
`query` and `brand`, matches whole words of a product's title or key without
accents or case (three letters or more also match a word's start, so `hd` does
not match `hdmi` but `ryz` matches `ryzen`), and answers at most 10 products with
their assessments, titles and the count of all matches. `price.assess` keeps its
earlier shape. The bridge passes `query` and `brand` in and hands the result back
as a structured `price` field, so the interface renders it in its own language;
the bridge writes no prices into reply text. Brands and kinds of product are
found because collectors put them in titles; the contract gained no field.

## Desktop view (implemented)

Library › Prices shows the same search as `/preco`: product and brand fields,
a filter by leading verdict, and one card per product with its sources, its
lowest price now and a chart of the market low over the last year. The chart
needs points a chat reply does not, so `price.search` returns each product's
market-low history (`market`, the last year, oldest first) only when the request
sets `history: true`; the Desktop shell always sets it and keeps only codes,
whole cents and days from the reply. Verdicts and signals are translated in the
Desktop catalogs.

## Watched searches (implemented: contract and storage)

The owner watches categories and brands, not single products: "ssd sata" from
"samsung", "hd" from any brand. What a person asks for decides the rest.

- **The domain owns the list** (`watches`: a category and an optional brand,
  normalized to lowercase words without accents, at most 50). Only the owner
  grows it: a `price.search` with a category from the Discord owner
  (`is_owner`) or from the Desktop, CLI or TUI adds it; anyone else gets an
  answer from what is already known, marked `not_owner`. `price.watches` lists
  the searches and `price.unwatch` (owner only) removes one.
- **The contract gains optional fields** in `price.observation.v1`:
  `category` and `brand` (the words of the watched search that found the
  product, so the domain never classifies anything), `part_number` (the
  manufacturer's code, the anchor that spans stores) and `url` (this
  merchant's page for the offer, shown to a person and never fetched by the
  domain). A description is replaced only by a non-empty value; the page is
  kept per history.
- **The search filters by store** (`merchant`), matches a brand against the
  product's brand when the collector gave one (else its title), returns each
  row's `url`, the product's description, every known merchant key, and what
  the search did to the watched searches (`watch.status`: added, watched,
  full or not_owner).
- **Schema version 2** adds the product description columns, `offers` (the
  page per history) and `watches`. `price-history-init` upgrades a version 1
  namespace in one transaction and keeps its points; until it runs, the
  orchestrator refuses the version 1 namespace and keeps history in memory.
- This replaces "the contract is the opt-in, so no list of sources is
  configured": collectors no longer pick what to follow.

## Store, links and opportunity order (implemented)

- `price.search` takes `order: "opportunity"`: real discounts first (deepest
  first), then ordinary prices by their discount against the 30-day median,
  then products still gathering history (cheapest first), and warnings last.
  Up to 300 matching products are assessed to rank them; the answer still
  holds 10. The Discord bridge always asks for it.
- `price.merchants` (`list-price-merchants`) lists the merchant keys with a
  history from one query, for a store picker.
- Discord's `/preco` gains `loja` with autocomplete, links each source to its
  store page and the card title to the cheapest offer, and says when the
  owner's search started a new watched search. The adapter now answers
  autocomplete interactions for any command that declares a suggester.

## Explicit watching and the Desktop (implemented)

- **Growing the list needs an explicit request.** `price.search` adds a
  watched search only when its payload sets `watch: true` (and the person may,
  as before). Discord's `/preco` is a deliberate command, so its bridge sets it;
  the Desktop searches as the owner types, so it sets it only from the "Watch
  this search" button, and a search typed on the way leaves no watch behind.
- **Desktop Library › Prices** gains a store picker (the search's merchant
  list), the "Watch this search" button with its outcome, the watched searches
  with removal (`price.watches`, `price.unwatch`), and the best-buy order.
- **Store pages stay in the shell.** The Desktop's projection replaces each
  offer's `url` with an opaque token; the shell keeps the newest 2,000 pages
  and opens one in the browser only for a token a price reply listed in this
  run, only for an http(s) address without credentials, and only on a real
  press (`dokja:open-offer`). The screen never sees an address, as the Desktop
  plan requires for every reply.

## Comparing one product (implemented)

`price.compare` (`compare-product`) lays one product's offers side by side.

- **Finding the product** is the domain's: an exact part number wins; a part
  number that only starts a stored one picks it when one variant matches and
  offers every variant as candidates when several do; without a part number,
  words that match one product pick it and several give candidates (at most 5).
  Offers come cheapest first, with the market low last as a reference.
- **A link is never fetched.** The orchestrator asks the resolver to
  `identify` it: the resolver reads the store, product id, title words and part
  number from the address text alone, and refuses an address of a store it
  does not know. Which stores it knows lives in the owner's plugin, not here.
- **A missing product** is looked up once (`lookup`, at most 3 products) when
  the person may grow what is collected (the same owner rule as watching), then
  compared again; anyone else is told it is not followed yet.
- **Ningo's comment** is written by the chat provider from the comparison's
  facts only, inside data markers and with the persona; it never supplies a
  number, is cut to 600 characters, and is left out when no model answers.
- Discord's `/comparar_produto produto:<name or part> link:<store link>`
  renders one card: the stores cheapest first with links, the comment, the
  part number, and a note when the link's store is not among the offers.

## Pending work (stacked follow-ups)


- **Collection runs with the stack.** Observations are recorded only while the
  owner's stack is up (`make start`); there is no separate host capture, so days
  the stack is down have no merchant points. The aggregator's rolling history
  can backfill the market minimum for those days.
- **Alerts** to a channel, and price commands in Desktop or CLI. These need the
  owner's approval of the surface before they are added.

## Compatibility

PR 1 adds a Go module and its CI step only. It changes no event envelope,
route, service contract, command, schema, environment variable or Compose file.
Collectors and their target addresses stay outside the repository. Tests and
documentation use invented products and merchants.
