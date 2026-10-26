# ADR 0014: adopt PostgreSQL and pgvector in application stores

Date: 2026-10-26. Status: accepted direction by the owner on 2026-10-25;
Knowledge backend implemented, cutover not performed. Memory remains pending.

## Decision

Prioritize PostgreSQL and pgvector adoption in the application, starting with
Knowledge and then Memory. The existing benchmark adapters are evidence, not
application storage implementations. Pause new receipt-store implementation
while establishing the PostgreSQL owners and migration path.

Knowledge retains its domain contracts and owns a PostgreSQL schema. Its first
adapter performs exact SQL cosine retrieval and database keyword search behind
the existing service API. Selecting PostgreSQL is explicit; preserve existing
SQLite data until a verified migration and reviewed cutover. Provide a verified
export path that includes writes accepted after cutover. No automatic fallback,
dual writing or production migration is authorized by implementing this backend.

The [runtime and migration contract](../knowledge-postgres.md) describes schema,
configuration, validation and rollback. All affected base Compose files project
the storage variables; an optional private PostgreSQL overlay provides local
infrastructure. Deployments still require owner approval.

## Relationship to prior decisions

This authorizes application adoption beyond ADR 0009's research-only status for
the existing Knowledge consumer. It does not settle every graph workload or
complete the bounded multi-hop comparison. It also does not change the
`Interface -> Event -> Orchestrator -> Domain -> Service` flow, make services
policy owners, infer scope for old unscoped records, or move conversation and
Runtime settings data. Memory and durable delivery ownership require their own
reviewed adapters and migrations.
