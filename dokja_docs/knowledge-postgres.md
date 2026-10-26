# Knowledge on PostgreSQL and pgvector

The owner prioritized application adoption on 2026-10-25: Knowledge first, then
Memory. This is an application backend, distinct from the synthetic storage
benchmarks. The Knowledge domain, orchestrator routes and `knowledge.*` requests
keep their current responsibilities. Memory and delivery receipt storage have
not migrated in this batch.

## Runtime contract

Set `KNOWLEDGE_STORAGE_BACKEND=postgres` and configure
`KNOWLEDGE_POSTGRES_DSN` in the operator's ignored environment file. The default
remains `sqlite` so an existing installation does not switch to an empty store.
`KNOWLEDGE_DB_FILE` continues to select the SQLite file when that backend is
chosen. PostgreSQL connection failure is an error; there is no fallback to
SQLite, automatic import or dual write. `knowledge.status` adds
`storage_backend`, so the operator can verify which store is serving requests.

The PostgreSQL service-owned schema is `dokja_knowledge`, version 1. Documents,
chunks, metadata, tags, source references and model-tagged embeddings persist
there. The extensions `vector` and `unaccent` must be available in `public`;
startup creates them when permitted. On a managed server, provision extensions
with an administrator and give the application ownership of its own schema.
Schema creation is transactional and serialized; unsupported versions are
refused. This does not change any existing SQLite schema version.

Vector candidates are selected by exact cosine search in PostgreSQL, filtered
by model, dimension and the existing kind restriction before ranking. The
application does not load the PostgreSQL embedding matrix into NumPy. Keyword
search uses an accent-insensitive `simple` text-search configuration and a GIN
index, then feeds the existing rank-fusion and relevance logic. Keyword ordering
can differ from SQLite BM25. No HNSW or approximate-recall claim is made. Mixed
model dimensions and zero vectors are handled without cross-model retrieval.

Replacement removes old chunks atomically; foreign keys remove metadata and
vectors on deletion. Metadata-only patches retain embeddings and compare the
existing content/metadata revision under a row lock. Two writers cannot both
apply different answers against one revision. Classification samples use a
repeatable-read snapshot. A second service connection sees fresh SQL results
without an application-side matrix cache.

## Provision and import

The optional `docker-compose.postgres.yml` overlay adds a PostgreSQL 17 image
with pgvector, a private Compose-network endpoint and the independent
`dokja-postgres-data` volume. It publishes no database host port. Set
`DOKJA_POSTGRES_PASSWORD` in the ignored environment file before using it.
The supplied local database and role are both named `dokja`; configure the DSN
for that role at `dokja-postgres:5432`, including authentication. External
PostgreSQL is also supported through the DSN without this overlay. Runtime's
module inventory does not manage the new infrastructure dependency; start it
explicitly before selecting the backend.

1. Keep `KNOWLEDGE_STORAGE_BACKEND=sqlite` while preparing. Configure the
   PostgreSQL DSN, rebuild the Knowledge image, and start only the database:

   ```sh
   docker compose -f docker-compose.lite.yml -f docker-compose.postgres.yml build dokja-knowledge
   docker compose -f docker-compose.lite.yml -f docker-compose.postgres.yml up -d dokja-postgres
   ```

2. Stop Knowledge and every writer. Take a consistent SQLite backup using the
   SQLite backup API, including committed WAL state. Preserve the original file.
   Keep writers stopped until cutover validation finishes.

3. Import explicitly into an empty PostgreSQL Knowledge schema:

   ```sh
   docker compose -f docker-compose.lite.yml -f docker-compose.postgres.yml run --rm --no-deps dokja-knowledge python migrate_postgres.py --sqlite-source /workspace/.dokja/dokja_knowledge.db
   ```

   Use the dev base for development. For production, use the prod base and
   `/data/dokja_knowledge.db`. The importer opens schema-1/2 SQLite read-only,
   copies document/chunk IDs, text, metadata, hashes, timestamps, vectors and
   model tags, and checks a full logical digest before committing. Metadata for
   schema-1 sources defaults to `{}` without upgrading the source. A nonempty
   target is refused; reruns never overwrite existing data. Invalid vectors or
   verification failure abort the import. The target transaction excludes writes
   throughout copying and verification. Embeddings are copied, not recomputed.

4. Require `verified: true`. With writers still stopped, set
   `KNOWLEDGE_STORAGE_BACKEND=postgres`, recreate Knowledge and inspect status,
   document counts, representative searches and classification metadata. Check
   the additive `storage_backend: postgres` status before resuming writes. This
   operation is a deployment and requires the owner's approval; none was
   performed by implementation or tests.

Import/export currently materializes a snapshot in memory; size the migration
process for the corpus. The integration fixtures do not establish a large-corpus
migration time or resource budget. Take independent PostgreSQL backups after
cutover; the old SQLite snapshot no longer contains subsequent writes.

## Rollback including new writes

Before PostgreSQL accepts new writes, the preserved SQLite backup can be selected
again. After new writes, stop all writers and export a new verified snapshot:

```sh
docker compose -f docker-compose.lite.yml -f docker-compose.postgres.yml run --rm --no-deps dokja-knowledge python migrate_postgres.py --sqlite-export /workspace/.dokja/dokja_knowledge.rollback.db
```

Use `/data/dokja_knowledge.rollback.db` in production. Export reads one
repeatable-read PostgreSQL snapshot, builds SQLite schema 2 with its keyword
index and original vectors, verifies the full digest, and publishes a new
owner-readable file atomically. An existing destination is never replaced.
Failure leaves the PostgreSQL source intact and does not publish a partial
database. Keep the original SQLite and PostgreSQL data. Before cutback, select
the exported file through the existing SQLite storage configuration; Compose
uses its existing fixed file path, so preserve that original and install the
verified replacement there while Knowledge is stopped. Only then switch back
to SQLite and validate before resuming writes. Reverting the backend alone after
cutover would lose access to the writes accepted in PostgreSQL.

## Validation and remaining work

Integration tests use a new disposable database per test. They cover application
bootstrap and the existing request envelope, SQL vector retrieval, keyword
fallback with accents, kind/model/dimension isolation, replacement/deletion,
fresh cross-connection reads, metadata revision races, preserved source files,
legacy import, full-digest failure rollback, post-cutover rollback export and
private database diagnostics. Live model validation remains opt-in. CI runs
both SQLite regressions and PostgreSQL integration fixtures.

This does not implement the cognitive graph, new ownership/scope semantics,
Memory adoption, delivery attempts or a cross-service transaction. Those remain
explicit subsequent batches in `steps.json`.
