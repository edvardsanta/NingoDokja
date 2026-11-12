# Technical benchmark collector

This disabled example implements `price.technical.v1` with action `collect`.
It reads operator-selected HTML tables and returns evidence for the requested
CPU/GPU models. It uses only Python's standard library and never calls an LLM.
Actual publisher URLs and selectors belong to an owner's ignored `settings.json`.

## Configure

Copy this directory into the existing Feeds plugin directory. Copy
`settings.example.json` to `settings.json`, choose permitted public report URLs,
and configure exact captions and headers from those reports. Set `enabled` to
`true` in the copied manifest after reviewing the configuration. Rebuild Feeds
and the orchestrator for the new capability; restart Feeds to discover the plugin.
Do not enable the shipped example with its placeholder source.

Each source entry selects one table. `model_column` is a zero-based column index.
Each metric selects another column, one explicit unit (`fps`, `percent_index`,
`score`, `seconds` or `ms`) and a full-match regex with exactly one numeric capture.
For example, a cell `75.2% (87.4)` can use
`[0-9]+(?:\.[0-9]+)?%\s*\(([0-9]+(?:\.[0-9]+)?)\)` to retain the measured
parenthesized value, provided the publisher explicitly defines that value's unit.
`higher_is_better` defaults to true; set false for latency or elapsed time.

`condition_terms` selects original paragraphs/list items describing the test
system, software, aggregation or procedure. Choose specific phrases from the
methodology, not product recommendations. Without extracted conditions the
collector retains the source row but emits no numeric measurement. Publication
and modification dates come from the page's article metadata; missing dates
remain unknown. Collection time is recorded separately.

The same URL is fetched at most once per request even when several tables are
configured. Different table snapshots and raster/ray workloads remain distinct.
Changing captions/headers, duplicate sample variants, known incompatible VRAM,
dual-power cells, conflicting single/multi-thread labels and missing numeric
values never silently select a nearby sample or a first number.
Board-specific OC, cooling and firmware are not inferred from model-level tests.

## Contract

Input on stdin:

```json
{
  "operation": "price.technical.v1",
  "action": "collect",
  "payload": {"products": [{"product": "example-gpu", "title": "RX 7600 8GB"}]}
}
```

Output on stdout is `{"result":{"evidence":[...],"failed_sources":0}}`.
Each evidence row has `product_key` and `technical_sources`. A source includes
its original excerpt, URL, dates, workload and test conditions. `benchmarks`
contains the test label, numeric value, unit, direction, tested sample and SHA256
snapshot of the table and conditions. No performance judgment or purchase
recommendation is returned. The Price domain validates samples and metadata,
calculates paired differences and determines how evidence may be used.

Requests accept at most 13 products and eight configured tables. Reads permit
only public HTTP(S) destinations on their standard ports, reject credentials and
mixed private/public DNS, pin validated addresses and retain TLS hostname
verification. Redirects are revalidated. Each page is capped at 4 MiB and each
socket at four seconds; the Feeds host enforces the 20-second plugin limit.
There is no proxy inheritance, script execution, image OCR or chart-image parsing.
Blocked or changed sources are reported as failed; their contents are not guessed.

Run offline regression tests from the repository root:

```sh
python3 -m unittest discover -s dokja_services/dokja_feeds/plugins.example/technical-benchmarks -p 'test_*.py' -v
```

Feeds' Go suite runs these tests as well. They use fictional reports, not live
publisher dependencies.
