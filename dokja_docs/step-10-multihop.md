# Step 10.1: bounded multi-hop correctness

## Consumer and scope

The first intended W2 consumer inspects a claim before proposing an action:
which evidence supports or contradicts it, and which dependencies connect them?
This is an isolated storage experiment, not a production reasoning feature.
W4 (method, procedure, episode, observed outcome) remains a separate workload.
No application database, public contract, configuration or deployment changes.

## Query contract

A node ID identifies one immutable version. The fixture records scope, current
version eligibility and deletion. Directed edges are `supported_by`,
`contradicted_by` and `depends_on`: traversal starts at the claim and follows
outgoing edges. Edges inherit visibility from both endpoints. Shared/public
scope and historical as-of retrieval are outside this first contract.

Return simple paths, preserving every edge label. Do not collapse opposing
assertions or turn transitive reachability into proof. Exclude an ineligible
root; exclude every path through an ineligible intermediate node, even when a
later endpoint is visible. Cycles cannot repeat a node within a path; distinct
paths to the same endpoint remain distinct. Duplicate identical edges collapse.

The deterministic order is breadth-first, then ascending destination ID and
relationship label within each expansion. Depth is 1–6. Result count and
examined-edge budget are required positive bounds. One additional eligible
path establishes result truncation. If the examination budget is exhausted
before that determination, return an error rather than a partial answer.
Caller cancellation likewise returns no partial answer. These bounds apply to
traversal after fixture preparation, not to loading an arbitrarily large graph.

## First batch: reference and independent expectations

`dokja_store/spikes/cognitive_storage/multihop` contains a Go reference traversal
and hand-written expected paths for 1, 3 and 6 hops. The fixture covers opposing
assertions, a diamond, a cycle, hidden intermediate nodes, deleted and superseded
versions. Additional tests cover a 10,000-edge hub, explicit truncation, expansion
budget failure, cancellation, malformed edges and input-order independence.

This establishes a correctness oracle for subsequent storage adapters. It does
not establish performance or correctness of any SQL or Cypher implementation.

## Remaining validation

1. Implement isolated SQLite, PostgreSQL and Neo4j adapters against this contract.
2. Compare their full ordered paths and truncation/error behavior with the oracle
   and hand-written expectations. Add engine timeout and cancellation checks.
3. Run matched synthetic fixtures at 1/3/6 hops, recording p50/p95/p99, resource
   use, graph size, fanout, cache state and concurrency. Report skipped engines.
4. Define W4 separately, retaining frozen predictions and observed outcomes.
5. Revisit ADR 0009 only after measurements. W1 remains vector-search evidence;
   the current W2 reference does not justify adopting or migrating a database.
