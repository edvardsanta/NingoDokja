# Step 10: storage spike, W1

Date: 2026-10-20. Status: first consumer measured; no engine adopted.
Decision record: [ADR 0009](./decisions/0009-cognitive-storage-evaluation.md).
Protocol: [storage research](./cognitive-storage-research.md#reproducible-comparison-plan).
Code: [`dokja_store/spikes/cognitive_storage`](../dokja_store/spikes/cognitive_storage),
a separate Go module so the database drivers stay out of `dokja_store`.
Raw results: [`results/`](../dokja_store/spikes/cognitive_storage/results).

## Question

The owner accepted W1 as the graph's first consumer (2026-10-20): find evidence
about an entity in the caller's scope, filtered by form, content kind and date,
ranked by vector similarity, citing only current versions. It is what step 09
started to classify (forms, content kinds, entities, date anchors) and what
Knowledge search already does.

**W1 does not tell whether Dokja needs a graph engine.** It is a filtered vector
search with one hop (document → entity). The workloads that would favour a graph,
W2 (what supports or contradicts a claim) and W4 (method → procedure → episode →
outcome), still have no consumer, and this spike does not invent one. The scope
and durability requirements and ADR 0003's hosting rule are also not decided
here: scope is fixture data (two scopes), and nothing about durability was
measured.

## Setup

- **Fixture:** generated from a seed, no owner data.
  - Documents carry a form (Knowledge's 13 forms, with `law` rare), a content
    kind, a date anchor for news, and one to four entity mentions. One entity is
    mentioned by a quarter of all documents.
  - About 5% of owner documents have a guest twin with near-identical vectors (a
    scope leak would rank high), 5% have a superseded version with near-identical
    vectors, and 2% are deleted after loading through each engine's own delete.
  - 200 queries per run, aimed near existing chunks: the high-degree entity, the
    rare form, a form, a content kind, news within a date range, or an entity
    alone.
- **Gold:** computed in Go from the fixture itself. No engine under test is the
  reference. Exact means the same chunks in the same order, with distances within
  1e-4. Recall@10 counts gold chunks returned; "short" counts answers with fewer
  than min(10, eligible) rows.
- **Engines:**
  - SQLite 3.53.4 (modernc.org/sqlite, in process). Filters and the mention
    join run in SQL; the vector scan runs in Go over the eligible chunks. It is
    close in spirit to Knowledge's application-side scan, but not Knowledge's
    Python/numpy code. No vector index.
  - PostgreSQL 17.11 with pgvector 0.8.7, image `pgvector/pgvector:pg17`
    (digest `sha256:ac08538c…`), `shared_buffers=2GB`. HNSW
    `vector_cosine_ops`, m=16, ef_construction=64, `ef_search=100`; approximate
    search with and without `iterative_scan = strict_order`. Exact search orders
    by an expression the index cannot serve; approximate search orders by the
    indexed operator alone and lets the planner choose.
  - Neo4j 2026.09.0 Community, image `neo4j:community` (digest
    `sha256:d6269a02…`), 4 GB heap, 2 GB page cache. `(:Doc)-[:MENTIONS]->(:Entity)`,
    `(:Doc)-[:HAS_CHUNK]->(:Chunk)`. Exact search computes
    `vector.similarity.cosine` over the traversal; approximate search calls
    `db.index.vector.queryNodes` with 100 candidates and filters what it returns.
    Its plan starts with the procedure call.
- **Host:** AMD Ryzen 7 5800X (16 threads), 46 GB RAM, NVMe, Linux, rootless
  podman, both servers on loopback. One client, warm caches (one warm-up pass,
  then a timed pass). No concurrency, cold-cache, restart or restore tests.

## Results

Latencies in milliseconds over 200 queries. Every run of every engine and mode
returned **no foreign-scope, superseded or deleted rows**. Every exact mode
matched the gold on all 200 queries.

| Chunks · dim | Engine | Mode | Exact | Recall@10 | Short | p50 | p95 | p99 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 10.7k · 64 | SQLite | exact | 200 | 1.00 | 0 | 0.1 | 3.3 | 3.9 |
| | PostgreSQL | exact | 200 | 1.00 | 0 | 0.5 | 1.8 | 2.0 |
| | PostgreSQL | approx | 200 | 1.00 | 0 | 0.5 | 1.8 | 2.1 |
| | Neo4j | exact | 200 | 1.00 | 0 | 0.7 | 7.7 | 8.8 |
| | Neo4j | approx | 49 | 0.48 | 113 | 1.6 | 1.9 | 2.1 |
| 108k · 64 | SQLite | exact | 200 | 1.00 | 0 | 0.3 | 34.7 | 37.0 |
| | PostgreSQL | exact | 200 | 1.00 | 0 | 1.4 | 12.8 | 13.3 |
| | PostgreSQL | approx | 200 | 1.00 | 0 | 1.3 | 12.1 | 12.3 |
| | Neo4j | exact | 200 | 1.00 | 0 | 1.2 | 70.3 | 74.9 |
| | Neo4j | approx | 30 | 0.33 | 130 | 3.9 | 4.9 | 5.8 |
| 10.8k · 1024 | SQLite | exact | 200 | 1.00 | 0 | 0.2 | 10.9 | 11.2 |
| | PostgreSQL | exact | 200 | 1.00 | 0 | 0.8 | 8.6 | 9.0 |
| | PostgreSQL | approx | 200 | 1.00 | 0 | 0.8 | 5.2 | 5.5 |
| | Neo4j | exact | 200 | 1.00 | 0 | 1.9 | 88.4 | 91.6 |
| | Neo4j | approx | 52 | 0.52 | 95 | 2.5 | 3.3 | 3.9 |
| 108k · 1024 | SQLite | exact | 200 | 1.00 | 0 | 0.9 | 111.1 | 114.9 |
| | PostgreSQL | exact | 200 | 1.00 | 0 | 2.0 | 95.1 | 97.5 |
| | PostgreSQL | approx | 200 | 1.00 | 0 | 1.9 | 61.8 | 64.8 |
| | Neo4j | exact | 200 | 1.00 | 0 | 6.5 | 850.3 | 853.6 |
| | Neo4j | approx | 15 | 0.23 | 138 | 6.4 | 7.1 | 7.8 |

PostgreSQL with `iterative_scan` matched plain approximate search within 1 ms at
every size; it is in the JSON files. Neo4j's approximate results vary by a query
or two between runs (HNSW build), as two runs of the first size showed.

At 108k chunks and 1024 dimensions: loading took 2.2 s (SQLite), 59 s
(PostgreSQL, through a text staging column this harness uses; not an engine
limit) and 11.9 s (Neo4j). Index builds took 105 s (PostgreSQL) and 21 s (Neo4j).
Sizes: 0.50 GB (SQLite file), 1.47 GB (PostgreSQL database) and 3.4 GB (Neo4j
`/data`, after every run, including transaction logs; not comparable).

## What it shows

- **All three answer W1 correctly when they scan.** None needed an index to be
  right, and none leaked across scope, version or deletion under the fixture's
  traps.
- **PostgreSQL's planner kept approximate search correct.** It read the HNSW index
  only where the filter was broad (usually the high-degree entity), and filtered
  first elsewhere. With the index it still found every gold chunk. Its exact p95
  was the lowest at every size, and 95 ms at the largest.
- **Neo4j's vector index is the wrong first step for W1.** It ranks the whole
  corpus, and scope, entity and form filters then drop most of the 100
  candidates: recall@10 of 0.23 to 0.52 and 95 to 138 short answers out of 200.
  Exact traversal is correct but its p95 reached 850 ms at the largest size,
  above the provisional 250 ms target. Raising the candidate count or the newer
  filtered vector search were not tried.
- **SQLite with an application scan stayed under the provisional target**
  (111 ms p95 at 108k · 1024) and grows with the eligible set. Its tail, like every
  engine's, comes from queries whose entity is the high-degree one.
- For W1 alone, nothing here argues for a graph engine. That is not evidence
  against one: the graph-shaped workloads were not measured.

## Limits

- Only W1, warm, one client. The protocol's W2–W9, concurrency, cold cache,
  kill/restart and restore are not run.
- No 1m-chunk rung. At 1024 dimensions it needs about 4 GB of raw vectors per
  copy; it was left for a run with the owner's go-ahead on time and disk.
- Generated vectors are uniform on the sphere. Real embeddings cluster, which
  changes HNSW recall; model quality is outside this spike.
- The 250 ms target is the research's provisional number, not an owner-validated
  budget.

## Next

- Name a consumer for W2 or W4 before benchmarking traversal; W1 cannot stand in.
- Decide the scope and durability requirements and ADR 0003's hosting rule;
  W6–W9 depend on them.
- If W1 alone drives the next step, PostgreSQL+pgvector is the only candidate that
  was both correct with its index and fastest at the largest size. SQLite remains
  acceptable at this size. No migration is proposed or authorized by this record.

## Reproduce

```sh
podman run -d --name spike-postgres -p 127.0.0.1:55432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust \
  --shm-size=1g docker.io/pgvector/pgvector:pg17 -c maintenance_work_mem=1GB -c shared_buffers=2GB
podman run -d --name spike-neo4j -p 127.0.0.1:57687:7687 -e NEO4J_AUTH=none \
  -e NEO4J_server_memory_heap_max__size=4G -e NEO4J_server_memory_pagecache_size=2G \
  docker.io/library/neo4j:community
cd dokja_store/spikes/cognitive_storage
SPIKE_POSTGRES_DSN=postgres://postgres@127.0.0.1:55432/postgres SPIKE_NEO4J_URI=bolt://127.0.0.1:57687 \
  go test ./...
go run ./cmd/w1bench -chunks 100000 -dim 1024 -out results/w1-100k-d1024.json \
  -postgres postgres://postgres@127.0.0.1:55432/postgres -neo4j bolt://127.0.0.1:57687
podman rm -f spike-postgres spike-neo4j
```

Both servers accept connections only from this machine and without a password;
do not publish them elsewhere.
