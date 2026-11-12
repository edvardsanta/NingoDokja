# Dokja Data Studio

The Data Studio is an administrative workspace inside the existing desktop application.
It lets the operator inspect SQLite databases, PostgreSQL connections, JSON
configuration files and Markdown documentation without leaving the Ningo desktop.

It is **not** a separate application and it never accesses files or databases
directly. All reads go through the orchestrator, keeping the repository's layered
architecture intact: `Interface → Orchestrator → Infrastructure`.

## Scope

- Discover and list data sources in the checkout directory.
- Open SQLite databases in read-only mode and inspect schema, tables and rows.
- List configured PostgreSQL connections (DSNs come from the orchestrator's
  environment; credentials are never sent to the screen).
- Read JSON configuration files as structured values.
- Read and edit Markdown documentation files, including persona files.
- Run diagnostics that report missing or unreadable sources.
- Search and filter within the currently opened resource.

Out of scope for the first version:

- PostgreSQL query execution (connections are listed and schema is browsable;
  live query execution needs a running server and is not yet wired).
- Writing to SQLite or JSON files.
- Global search across all sources.
- Automatic backups before edits.

## Architecture

```
dokja_interfaces/desktop/src/renderer/cards/data_studio.tsx
                |
                v
        data_studio_model.ts
                |
                v
        IPC actions (allow-listed)
                |
                v
   dokja_orch/internal/handlers/data_studio_handler.go
                |
                v
   dokja_orch/internal/infrastructure/data_studio/*
```

### Desktop side

- `src/renderer/cards/data_studio.tsx` renders the workspace.
- `src/renderer/cards/data_studio_model.ts` builds the payloads and calls the
  shared desktop transport.
- Labels live in `src/renderer/i18n/locales/en.json` and `pt.json`.
- The card is registered in `src/renderer/cards/registry.ts`.
- Actions are allow-listed in `src/shared/actions.ts` and forwarded by
  `src/main/actions.ts`.

### Orchestrator side

- `dokja_orch/internal/core/event.go` defines `DomainDataStudio`.
- `dokja_orch/internal/core/router.go` routes `datastudio.*` actions.
- `dokja_orch/internal/core/workflow_engine.go` maps actions to handler calls.
- `dokja_orch/internal/handlers/data_studio_handler.go` implements the action
  handlers and projects replies for the screen.
- `dokja_orch/internal/infrastructure/data_studio/` contains read-only adapters:
  - `discovery.go` scans the checkout directory for SQLite, JSON, Markdown and
    persona files.
  - `sqlite_reader.go` opens SQLite databases read-only and runs bounded SELECTs.
  - `postgres_reader.go` connects to PostgreSQL and lists schemas/tables.
  - `util.go` provides file reading helpers and SQL allow-listing.

The handler is wired in `dokja_orch/cmd/va/main.go` with the PostgreSQL DSNs read
from environment variables.

## Actions

| Action | Direction | Purpose |
| --- | --- | --- |
| `datastudio.sources` | read | List all discovered sources and Postgres connections. |
| `datastudio.sqlite.schema` | read | Schema objects of a SQLite database. |
| `datastudio.sqlite.table` | read | Columns and row count for one SQLite table. |
| `datastudio.sqlite.rows` | read | Paginated rows from a SQLite table. |
| `datastudio.sqlite.query` | read | Run a bounded, allow-listed SELECT. |
| `datastudio.json.read` | read | Read and parse a JSON file. |
| `datastudio.markdown.read` | read | Read a Markdown or persona file. |
| `datastudio.markdown.write` | change | Save a Markdown file after presence confirmation. |
| `datastudio.postgres.schemas` | read | List schemas for a configured Postgres connection. |
| `datastudio.postgres.tables` | read | List tables/views inside a schema. |
| `datastudio.postgres.table` | read | Columns and row count for one Postgres table. |
| `datastudio.diagnostics` | read | Report accessibility problems for known sources. |

All actions go through the existing orchestrator envelope and obey the desktop's
allow-list and presence checks for changes.

## Security

- SQLite is opened with `mode=ro` and a single connection. The adapter rejects
  any statement that does not start with `SELECT` and rejects strings containing
  semicolons to prevent multi-statement injection.
- File paths are validated against the checkout directory; paths outside the base
  directory or using `..` are rejected.
- PostgreSQL DSNs are read by the orchestrator from environment variables. The
  screen receives only connection names, database names and schema/table metadata.
- Markdown writes require the desktop's change gesture and a confirmation prompt.
- The Data Studio does not run migrations, execute files, or follow symlinks
  outside the base directory.

## Testing

- `dokja_orch/internal/infrastructure/data_studio/*_test.go` cover discovery,
  SQLite read-only access, SQL allow-listing and file helpers.
- `dokja_orch/internal/core/admin_routes_test.go` pins the `datastudio.*` routes
  and workflow actions.
- `dokja_interfaces/desktop` tests include the new workspace in the sidebar list
  and keep the i18n catalogs synchronized.

Run the relevant suites:

```bash
cd dokja_orch && go test ./... && go build ./...
cd dokja_interfaces/desktop && pnpm test && pnpm typecheck && pnpm build
```

Verified end to end on 2026-11-11 against a local orchestrator: the
`datastudio.*` actions answered over the ZeroMQ request socket with the real
checkout data — `datastudio.sources.list` found 30 sources, a read-only SELECT
returned live rows from `ningo_memory.db`, a `DELETE` was refused with "only
SELECT statements are allowed", and a path outside the checkout
(`/etc/passwd`) was rejected by the orchestrator's path validation.

## Known limitations

- PostgreSQL browsing needs a reachable server and is only tested manually.
- Global unified search across SQLite/Postgres/JSON/Markdown is not implemented.
- SQLite writes, JSON editing and automated backups are planned for a later
  refinement pass.
- The Markdown editor does not yet show a rendered preview or diff view.

## Future work

- Add a PostgreSQL query runner with timeouts and cancellation.
- Implement global search with local indexing and source scoping.
- Add structured JSON editing with schema validation and change preview.
- Add safe, opt-in write modes for SQLite and JSON with backups and transactions.
