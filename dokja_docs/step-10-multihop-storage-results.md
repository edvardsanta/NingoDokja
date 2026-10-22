# Step 10.1: storage-backed W2 traversal

Measured on 2026-10-22. This is the first three-engine graph traversal baseline,
independent of PR #83's model-assisted inference experiment. Step 10.1 remains
in progress because W4, native traversal alternatives and broader workload
validation remain open. No engine adoption or application migration is proposed.

## Implemented contract

`multihop.Traverse` performs identical breadth-first simple-path enumeration
against immutable fixture readers for SQLite, PostgreSQL and Neo4j. Readers page
indexed outgoing adjacency, ordered by target ID and relationship kind. They
return target eligibility explicitly: hidden-target edges consume examination
budget even though they never become result paths. Pages are bounded by remaining
examination/result budgets and capped at 128 edges. Duplicate edges collapse at
fixture preparation, while opposing labels and distinct paths are retained.

This measures a shared client-controlled traversal policy and its SQL/Cypher
adjacency access. It does not compare optimized recursive CTEs or native
variable-length Cypher traversal. It cannot establish which engine has the best
native graph execution plan. Reference traversal is a separate in-memory BFS;
hand-written expected paths independently validate both implementations.

SQLite keeps the fixture in an in-memory database. PostgreSQL uses indexed
connection-local temporary tables and creates no extensions. Neo4j uses a random
run namespace, a retained composite run/ID index and run-scoped batched cleanup.
Readers support one caller at a time. Other fixture runs survive cleanup; no
application tables are read, reset or migrated. Use disposable external databases.

## Conditions

- Go 1.25.0; modernc.org/sqlite v1.59.0; PostgreSQL 17.11 from the pgvector image;
  Neo4j Community 2026.09.0. W2 uses relational/graph operations, not vectors.
- Local Linux, Ryzen 7 5800X (8 cores/16 threads), about 50 GB visible memory.
  Four application containers remained running; host/background load was not
  controlled. Container engines were accessed through loopback using Podman.
- PostgreSQL data and Neo4j data were disposable tmpfs mounts. Neo4j configured
  512 MB maximum heap and 256 MB page cache. Results do not represent disk durability.
- Forests contain 127 nodes and 129 edges per component, binary branching through
  depth 6, a diamond, opposing labels and a cycle. Every thirteenth nonroot node
  is foreign-scope, seventeenth superseded, nineteenth deleted. Version identity
  is the immutable node ID. Up to eight roots are queried round-robin.
- 100 measured queries per depth per engine/size; five warmups per depth; one
  caller; no cache eviction. Limit 256 paths, 10,000 examined edges, five-second
  query deadlines. Each result, including order and truncation, is compared with
  the reference outside query timing. All 2,700 matched; none truncated in these
  fixtures. High-degree/truncation/budget cases are separate correctness tests.
- Engines/sizes ran sequentially. Neo4j was rerun after fixing cleanup; its
  final series ran after container recreation. JVM/cache warmup can explain why
  the larger graph series is faster; the results do not establish a scaling law.

## Query latency

All cells are p50 / p95 / p99 in milliseconds, nearest-rank percentiles. At 100
samples the p99 is a tail observation, not a statistically stable tail estimate.

| Engine | Nodes / edges | 1 hop | 3 hops | 6 hops |
| --- | ---: | ---: | ---: | ---: |
| sqlite | 127 / 129 | 0.063 / 0.095 / 0.103 | 0.457 / 0.583 / 0.661 | 3.525 / 4.942 / 5.066 |
| sqlite | 12,700 / 12,900 | 0.070 / 0.109 / 0.223 | 0.554 / 0.836 / 0.874 | 3.784 / 4.705 / 5.377 |
| sqlite | 127,000 / 129,000 | 0.058 / 0.073 / 0.089 | 0.522 / 0.697 / 0.893 | 3.505 / 4.353 / 4.798 |
| postgres | 127 / 129 | 0.159 / 0.221 / 0.270 | 1.003 / 1.231 / 1.295 | 7.107 / 7.796 / 8.452 |
| postgres | 12,700 / 12,900 | 0.145 / 0.210 / 0.241 | 0.880 / 1.062 / 1.192 | 6.033 / 7.095 / 8.413 |
| postgres | 127,000 / 129,000 | 0.132 / 0.172 / 0.188 | 0.860 / 1.007 / 1.138 | 6.099 / 7.059 / 7.357 |
| neo4j | 127 / 129 | 3.644 / 5.761 / 6.844 | 9.640 / 14.314 / 16.207 | 38.932 / 47.627 / 49.011 |
| neo4j | 12,700 / 12,900 | 1.148 / 1.551 / 1.952 | 5.236 / 6.821 / 8.290 | 29.347 / 32.489 / 60.734 |
| neo4j | 127,000 / 129,000 | 0.704 / 0.830 / 0.875 | 4.230 / 4.918 / 5.828 | 28.437 / 30.867 / 31.895 |

SQLite's in-process adjacency baseline was faster in this sample. PostgreSQL's
loopback round trips and Neo4j's transaction/transport overhead are included.
This supports retaining both existing SQL options as viable W2 baselines; it
does not establish that a graph database is needed, or rule out better native
Neo4j/PostgreSQL plans. W1 vector-retrieval results remain a separate workload.

## Client resources

Peak RSS and CPU cover the entire client process, including fixture generation,
loading, reference computation, queries, verification and cleanup. CPU excludes
external database processes; SQLite executes inside the client. They are not
comparable total engine resource costs. Measurements used a fresh child process
and Python `resource.getrusage(RUSAGE_CHILDREN)`; RSS is Linux KiB converted below.
Allocation counts include client/driver and verification overhead, not server heap.

| Engine | Nodes | Load ms | Client peak RSS MiB | Client CPU user+system s | Allocated KiB/query at 6 hops |
| --- | ---: | ---: | ---: | ---: | ---: |
| sqlite | 127 | 1 | 20.7 | 0.697 | 238.2 |
| sqlite | 12,700 | 115 | 27.8 | 1.050 | 240.9 |
| sqlite | 127,000 | 1116 | 104.3 | 3.901 | 239.8 |
| postgres | 127 | 7 | 19.6 | 0.321 | 200.7 |
| postgres | 12,700 | 43 | 23.1 | 0.469 | 201.4 |
| postgres | 127,000 | 282 | 86.0 | 1.773 | 201.4 |
| neo4j | 127 | 1573 | 18.9 | 2.334 | 1303.9 |
| neo4j | 12,700 | 917 | 26.9 | 2.482 | 1305.9 |
| neo4j | 127,000 | 2544 | 116.4 | 3.914 | 1307.2 |

Initial container memory snapshots after correctness setup were about 66 MB for
PostgreSQL and 1.4 GB for Neo4j. These are cgroup-reported snapshots including
cache/other memory, not per-query or peak database RSS. Final snapshots after
the corrected run and cleanup were about 66 MB and 1.8 GB respectively. Database CPU, database
peak memory, cold-cache behavior, concurrent callers, connected graphs of large
reachable size and restart/durability behavior were not measured.

## Correctness and cleanup

The live three-engine race suite passed hand-written 1/3/6-hop results, cycles,
diamonds, opposing edges, hidden/deleted/superseded roots/intermediates, duplicate
edges, negative IDs, pagination through hidden targets, randomized graphs,
result truncation, exact-limit nontruncation, budget exhaustion with no partial
answer, cancellation before traversal and cancellation after a fetched page.
A 10,000-edge hub exercises early result/budget bounds in every adapter.

Cleanup regression tests preserve another run's data. The first large Neo4j
run exposed a cleanup timeout: one bulk delete exceeded ten seconds and the
runner ignored its deferred error. Cleanup now deletes batches under a bounded
30-second context, and the runner propagates cleanup errors. A regression loads
and removes 127,000 nodes, checks zero remaining nodes for that run and verifies
an independent sentinel fixture still answers correctly. The entire Neo4j
measurement series above was rerun after this fix. A final external zero-node
check and disposal of the isolated stack complete verification.

Commands passed in the spike module:

```sh
go test -race ./...
go vet ./...
W2_POSTGRES_DSN=postgres://postgres@localhost:55441/postgres W2_NEO4J_URI=bolt://localhost:57691 go test -race ./multihop -count=1
```

The default full suite runs SQLite; external graph tests run only when their
W2 endpoints are supplied. W1 external-engine tests were not rerun. Cancellation
of an already executing expensive native query and engine-side statement timeout
behavior remain untested; these readers issue bounded adjacency requests.

## Reproduction

Provision isolated PostgreSQL and Neo4j Community instances. Authentication and
endpoints are runtime inputs; never use application databases. Neo4j requires
index creation rights and leaves its named composite index until disposal.
From `dokja_store/spikes/cognitive_storage`:

```sh
go run ./cmd/w2bench -engine sqlite -trees 1000 -runs 100
W2_POSTGRES_DSN=postgres://postgres@localhost:55441/postgres go run ./cmd/w2bench -engine postgres -trees 1000 -runs 100
W2_NEO4J_URI=bolt://localhost:57691 go run ./cmd/w2bench -engine neo4j -trees 1000 -runs 100
```

Repeat with trees 1 and 100. The runner emits JSON with fixtures, cache/concurrency
conditions, correctness counts, latency and client allocation measurements.
Raw machine-specific investigation reports are disposable; the tables above
record this run. Production configuration, Compose stacks, event envelopes,
public HTTP contracts and service ownership are unchanged.

Next: compare native recursive execution under equivalent cancellation/budget
semantics, test larger connected reachable graphs and concurrency, define W4's
method/procedure/episode/observed-outcome consumer, then revisit ADR 0009 using
those results. No database adoption follows from this baseline alone.
