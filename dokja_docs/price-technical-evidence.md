# Collected technical price evidence

The comparison workflow can now collect actual benchmark rows before its fresh
technical review. It invokes the optional Feeds `price.technical.v1` capability,
a separate collector from store prices. The disabled reference implementation
and configuration instructions are in
[technical-benchmarks](../dokja_services/dokja_feeds/plugins.example/technical-benchmarks/README.md).
Publisher URLs are runtime configuration in the owner's ignored plugin files.

## Data and interpretation

The collector mechanically matches CPU/GPU models and returns original table
rows, publication/modification dates, collection time, test conditions and
measurements. It keeps model suffixes and refuses ambiguous sample matches.
A model-level result does not assert that the exact store board's OC, VRAM,
firmware or cooling was tested. Multiple power modes, contradictory headers and
unknown conditions remain source excerpts without automatic numeric comparison.
The first collector supports HTML tables, not image-only charts or arbitrary
web search. Missing models and publisher access failures remain explicit gaps.

The Price domain accepts only products already present in the comparison,
normalizes source fields and verifies that measured sample identities match.
It pairs metrics only when source URL, table snapshot, test, unit, direction and
reported conditions agree. Differences are computed as basis points from the
original values. A lower time/latency value is not treated as a higher score.
A paired result is scoped to that test; it does not change market-tier eligibility
or declare universal technical equivalence.

The analysis profile receives the collected evidence, actual test labels and
computed differences. Its interpretation remains a fresh qualitative estimate.
Source excerpts are untrusted data, not instructions. Citations must point to
collected URLs. Discord shows one concise model benchmark beside each candidate,
and source dates prefer the publisher's modification/publication date over the
collection date. All new labels use the existing English/Portuguese catalogs.

## Bounds and cache

Collection gets at most 20 seconds and preserves the existing review/comment
reserve inside the 165-second comparison deadline. It shares the Feeds extraction
slots and observes its availability control. An unavailable/disabled plugin,
source failure, malformed evidence or insufficient time leaves the price result
available. Partial source coverage does not become a claim about missing tests.

Validated source facts use the existing competitor SQLite cache under a
versioned product-set key. Only product identities and source facts are stored;
price/budget, model assessment and purchase recommendation are excluded. A cache
hit survives process restart and budget changes, but is rejected seven days after
collection. Publication date never refreshes that TTL. Future collection times
and malformed cached records are rejected. The existing settings/price schemas,
event envelope and command options do not change.

Source selection changes may require waiting for the bounded cache age or the
existing operator cache invalidation. Publisher layout changes require updating
selectors in the ignored configuration. The collector fails closed instead of
reinterpreting columns. Do not enable copied plugins until the source configuration
has been reviewed.

## Deployment and verification

Rebuild Feeds and the orchestrator, install/configure the optional collector in
the existing plugin directory, then rebuild Discord for benchmark/date rendering.
No new environment variable, port, volume, dependency service or table is added.
Without a configured collector, comparisons keep their existing estimate-only
behavior. A default checkout ships the example disabled and contains no real
publisher URLs or collector credentials.

Regression coverage includes table parsing, suffix/VRAM identity, contradictory
headers, power modes, date provenance, network boundaries, plugin dispatch,
source validation, same-snapshot arithmetic, cache reopen/budget independence,
collector outage/deadline handling and localized Discord output. Live collector
verification is an operator action; CI uses fictional reports and never relies
on a publisher being reachable.
