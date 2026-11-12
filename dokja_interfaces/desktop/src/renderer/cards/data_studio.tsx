import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import type { Transport } from "../../shared/transport.js";
import { useTranslate } from "../i18n/context.js";
import { Pending, Problem } from "./card_frame.js";
import { WorkspacePanel } from "./workspace_panel.js";
import { Button, Notice } from "./ui.js";
import { useCard } from "./use_card.js";
import type { Source, SourceKind, QueryResult, Diagnostics } from "./data_studio_model.js";
import {
  listSources,
  readSQLiteSchema,
  readSQLiteTable,
  querySQLite,
  readJSONFile,
  readMarkdownFile,
  runDiagnostics,
  listPostgresSchemas,
  listPostgresTables,
  readPostgresTable,
  queryPostgres,
  maskedDSN,
} from "./data_studio_model.js";

const KIND_ORDER: SourceKind[] = ["sqlite", "postgres", "json", "markdown", "persona"];

const KIND_LABELS: Record<SourceKind, string> = {
  sqlite: "SQLite",
  postgres: "PostgreSQL",
  json: "JSON",
  markdown: "Markdown",
  persona: "Persona",
};

export function DataStudioCard({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const [selected, setSelected] = useState<Source | null>(null);

  const load = useCallback(async () => {
    try {
      const sources = await listSources(transport);
      return { ok: true as const, data: sources };
    } catch (error) {
      return { ok: false as const, error: { code: "orchestrator" as const, message: String(error) } };
    }
  }, [transport]);

  const { state, reload, poll, refreshing } = useCard(load);

  const grouped = useMemo(() => {
    if (state.phase !== "ready") return [] as [SourceKind, Source[]][];
    const map = new Map<SourceKind, Source[]>();
    for (const kind of KIND_ORDER) {
      map.set(kind, []);
    }
    for (const source of state.data) {
      const list = map.get(source.kind) ?? [];
      list.push(source);
      map.set(source.kind, list);
    }
    return KIND_ORDER.map((kind) => [kind, map.get(kind) ?? []] as [SourceKind, Source[]]).filter(([, list]) => list.length > 0);
  }, [state]);

  return (
    <WorkspacePanel trail={[t("data_studio_title")]}>
      <div className="data-studio">
        <aside className="data-studio-sidebar">
          {state.phase === "loading" && !refreshing && <Pending />}
          {state.phase === "error" && (
            <>
              <Problem error={state.error} />
              <Button onClick={reload}>{t("retry")}</Button>
            </>
          )}
          {state.phase === "ready" && (
            <>
              <OverviewCounts sources={state.data} transport={transport} />
              {grouped.map(([kind, sources]) => (
                <details key={kind} open className="source-group">
                  <summary>{KIND_LABELS[kind]} ({sources.length})</summary>
                  <ul>
                    {sources.map((source) => (
                      <li key={source.id}>
                        <Button
                          variant={selected?.id === source.id ? "primary" : "quiet"}
                          size="compact"
                          onClick={() => setSelected(source)}
                        >
                          {source.name}
                          {source.size > 0 && kind !== "postgres" && (
                            <span className="source-size"> {formatSize(source.size)}</span>
                          )}
                        </Button>
                      </li>
                    ))}
                  </ul>
                </details>
              ))}
            </>
          )}
        </aside>
        <section className="data-studio-detail">
          {selected ? (
            <SourceDetail source={selected} transport={transport} onRefresh={poll} />
          ) : (
            <Notice boxed tone="unchecked">{t("data_studio_no_sources")}</Notice>
          )}
        </section>
      </div>
    </WorkspacePanel>
  );
}

function OverviewCounts({ sources, transport }: { sources: Source[]; transport: Transport }) {
  const t = useTranslate();
  const counts = useMemo(() => {
    const c: Record<SourceKind, number> = { sqlite: 0, postgres: 0, json: 0, markdown: 0, persona: 0 };
    for (const s of sources) c[s.kind]++;
    return c;
  }, [sources]);

  const chips: { kind: SourceKind; label: string }[] = [
    { kind: "sqlite", label: "SQLite" },
    { kind: "postgres", label: "PostgreSQL" },
    { kind: "json", label: "JSON" },
    { kind: "markdown", label: "Markdown" },
    { kind: "persona", label: t("data_studio_persona") },
  ];

  return (
    <div className="data-studio-overview">
      <div className="count-chips" role="list">
        {chips.map(({ kind, label }) => (
          <span key={kind} role="listitem" className="count-chip" data-kind={kind} aria-label={`${counts[kind]} ${label}`}>
            <strong>{counts[kind]}</strong> {label}
          </span>
        ))}
      </div>
      <DiagnosticsButton transport={transport} />
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${bytes} B`;
}

function DiagnosticsButton({ transport }: { transport: Transport }) {
  const t = useTranslate();
  const [result, setResult] = useState<Diagnostics | null>(null);
  const [running, setRunning] = useState(false);

  const run = async () => {
    setRunning(true);
    try {
      const diag = await runDiagnostics(transport);
      setResult(diag);
    } catch (error) {
      setResult({ sources: 0, issues: [{ kind: "sqlite", name: "diagnostics", error: String(error) }] });
    }
    setRunning(false);
  };

  return (
    <div>
      <Button onClick={run} disabled={running}>{t("data_studio_diagnostics")}</Button>
      {result && (
        <div className="data-studio-diagnostics">
          <p>{t("data_studio_count")}: {result.sources}</p>
          {result.issues.length === 0 ? (
            <Notice tone="ok" boxed>{t("data_studio_no_issues")}</Notice>
          ) : (
            <ul>
              {result.issues.map((issue, i) => (
                <li key={i}><strong>{issue.name}</strong>: {issue.error}</li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

function SourceDetail({ source, transport, onRefresh }: { source: Source; transport: Transport; onRefresh?: () => void }) {
  switch (source.kind) {
    case "sqlite":
      return <SQLiteDetail path={source.path!} transport={transport} onRefresh={onRefresh} />;
    case "postgres":
      return <PostgresDetail source={source} transport={transport} />;
    case "json":
      return <JSONDetail path={source.path!} transport={transport} />;
    case "markdown":
    case "persona":
      return <MarkdownDetail path={source.path!} transport={transport} />;
    default:
      return <Notice boxed tone="unchecked">{source.name}</Notice>;
  }
}

function SQLiteDetail({ path, transport, onRefresh }: { path: string; transport: Transport; onRefresh?: () => void }) {
  const t = useTranslate();
  const [selectedTable, setSelectedTable] = useState<string | null>(null);
  const [query, setQuery] = useState("SELECT * FROM ");
  const [queryResult, setQueryResult] = useState<QueryResult | null>(null);

  const load = useCallback(async () => {
    try {
      const schema = await readSQLiteSchema(transport, path);
      return { ok: true as const, data: schema };
    } catch (error) {
      return { ok: false as const, error: { code: "orchestrator" as const, message: String(error) } };
    }
  }, [transport, path]);

  const { state, reload } = useCard(load);

  const runQuery = async () => {
    try {
      const result = await querySQLite(transport, path, query);
      setQueryResult(result);
    } catch (error) {
      setQueryResult({ columns: [], rows: [], total: 0 });
    }
  };

  return (
    <div>
      <h3>{t("data_studio_sqlite")}: {path}</h3>
      <p className="read-only-badge">{t("data_studio_read_only")}</p>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && (
        <>
          <Problem error={state.error} />
          <Button onClick={reload}>{t("retry")}</Button>
        </>
      )}
      {state.phase === "ready" && (
        <>
          <details open className="schema-list">
            <summary>{t("data_studio_objects")} ({state.data.objects.length})</summary>
            <ul>
              {state.data.objects.map((obj) => (
                <li key={obj.name}>
                  <Button variant={selectedTable === obj.name ? "primary" : "quiet"} size="compact" onClick={() => setSelectedTable(obj.name)}>
                    {obj.type}: {obj.name}
                  </Button>
                </li>
              ))}
            </ul>
          </details>
          {selectedTable && <SQLiteTableDetail path={path} table={selectedTable} transport={transport} />}
          <div className="query-box">
            <label>{t("data_studio_query")}</label>
            <textarea value={query} onChange={(e) => setQuery(e.target.value)} rows={3} />
            <Button onClick={runQuery}>{t("data_studio_run")}</Button>
          </div>
          {queryResult && <ResultTable result={queryResult} />}
        </>
      )}
    </div>
  );
}

function SQLiteTableDetail({ path, table, transport }: { path: string; table: string; transport: Transport }) {
  const t = useTranslate();
  const load = useCallback(async () => {
    try {
      const schema = await readSQLiteTable(transport, path, table);
      return { ok: true as const, data: schema };
    } catch (error) {
      return { ok: false as const, error: { code: "orchestrator" as const, message: String(error) } };
    }
  }, [transport, path, table]);

  const { state, reload } = useCard(load);

  return (
    <div className="table-detail">
      <h4>{t("data_studio_table")}: {table}</h4>
      {state.phase === "loading" && <Pending />}
      {state.phase === "error" && (
        <>
          <Problem error={state.error} />
          <Button onClick={reload}>{t("retry")}</Button>
        </>
      )}
      {state.phase === "ready" && (
        <>
          <p>{t("data_studio_count")}: {state.data.count}</p>
          <table>
            <thead>
              <tr>
                <th>{t("data_studio_columns")}</th>
                <th>{t("data_studio_col_type")}</th>
                <th>{t("data_studio_col_not_null")}</th>
                <th>{t("data_studio_col_pk")}</th>
              </tr>
            </thead>
            <tbody>
              {state.data.columns.map((col) => (
                <tr key={col.name}>
                  <td>{col.name}</td>
                  <td>{col.type}</td>
                  <td>{col.not_null ? t("data_studio_yes") : t("data_studio_no")}</td>
                  <td>{col.primary_key ? t("data_studio_yes") : t("data_studio_no")}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}

function PostgresDetail({ source, transport }: { source: Source; transport: Transport }) {
  const t = useTranslate();
  const [schemas, setSchemas] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selectedSchema, setSelectedSchema] = useState<string | null>(null);
  const [query, setQuery] = useState("SELECT * FROM ");
  const [queryResult, setQueryResult] = useState<QueryResult | null>(null);
  const [queryError, setQueryError] = useState<string | null>(null);

  const load = async () => {
    setError(null);
    try {
      setSchemas(await listPostgresSchemas(transport, source.id));
    } catch (err) {
      setError(String(err));
      setSchemas(null);
    }
  };

  const runQuery = async () => {
    setQueryError(null);
    try {
      setQueryResult(await queryPostgres(transport, source.id, query));
    } catch (err) {
      setQueryResult(null);
      setQueryError(String(err));
    }
  };

  return (
    <div>
      <h3>{t("data_studio_postgres")}: {source.name}</h3>
      <p className="read-only-badge">{t("data_studio_read_only")}</p>
      <p className="postgres-dsn">{maskedDSN(source.dsn ?? "")}</p>
      {schemas === null && !error && <Button onClick={load}>{t("data_studio_connect")}</Button>}
      {error && (
        <>
          <Notice tone="problem" boxed announce="assertive">{error}</Notice>
          <Button onClick={load}>{t("retry")}</Button>
        </>
      )}
      {schemas !== null && (
        schemas.length === 0 ? (
          <Notice boxed tone="unchecked">{t("data_studio_no_schemas")}</Notice>
        ) : (
          <details open className="schema-list">
            <summary>{t("data_studio_schemas")} ({schemas.length})</summary>
            <ul>
              {schemas.map((schema) => (
                <li key={schema}>
                  <Button variant={selectedSchema === schema ? "primary" : "quiet"} size="compact" onClick={() => setSelectedSchema(schema === selectedSchema ? null : schema)}>
                    {schema}
                  </Button>
                  {selectedSchema === schema && (
                    <PostgresTables id={source.id} schema={schema} transport={transport} />
                  )}
                </li>
              ))}
            </ul>
          </details>
        )
      )}
      {schemas !== null && schemas.length > 0 && (
        <div className="query-box">
          <label>{t("data_studio_query")}</label>
          <textarea value={query} onChange={(e) => setQuery(e.target.value)} rows={3} />
          <Button onClick={runQuery}>{t("data_studio_run")}</Button>
        </div>
      )}
      {queryError && <Notice tone="problem" boxed announce="assertive">{queryError}</Notice>}
      {queryResult && <ResultTable result={queryResult} />}
    </div>
  );
}

function PostgresTables({ id, schema, transport }: { id: string; schema: string; transport: Transport }) {
  const t = useTranslate();
  const [tables, setTables] = useState<{ name: string; type: string }[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [selectedTable, setSelectedTable] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    setError(null);
    listPostgresTables(transport, id, schema)
      .then((list) => { if (alive) setTables(list); })
      .catch((err) => { if (alive) setError(String(err)); });
    return () => { alive = false; };
  }, [transport, id, schema]);

  if (error) return <Notice tone="problem" boxed announce="assertive">{error}</Notice>;
  if (tables === null) return <Pending />;
  if (tables.length === 0) return <Notice boxed tone="unchecked">{t("data_studio_no_tables")}</Notice>;

  return (
    <div className="postgres-tables">
      <ul>
        {tables.map((table) => (
          <li key={table.name}>
            <Button variant={selectedTable === table.name ? "primary" : "quiet"} size="compact" onClick={() => setSelectedTable(table.name === selectedTable ? null : table.name)}>
              {table.type}: {table.name}
            </Button>
            {selectedTable === table.name && (
              <PostgresTableColumns id={id} schema={schema} table={table.name} transport={transport} />
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function PostgresTableColumns({ id, schema, table, transport }: { id: string; schema: string; table: string; transport: Transport }) {
  const t = useTranslate();
  const [detail, setDetail] = useState<{ columns: { name: string; type: string; not_null: boolean; primary_key: boolean }[]; count: number } | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    setError(null);
    readPostgresTable(transport, id, schema, table)
      .then((schemaDetail) => { if (alive) setDetail(schemaDetail); })
      .catch((err) => { if (alive) setError(String(err)); });
    return () => { alive = false; };
  }, [transport, id, schema, table]);

  if (error) return <Notice tone="problem" boxed>{error}</Notice>;
  if (detail === null) return <Pending />;
  return (
    <div className="table-detail">
      <p>{t("data_studio_count")}: {detail.count}</p>
      <table>
        <thead>
          <tr>
            <th>{t("data_studio_columns")}</th>
            <th>{t("data_studio_col_type")}</th>
            <th>{t("data_studio_col_not_null")}</th>
          </tr>
        </thead>
        <tbody>
          {detail.columns.map((col) => (
            <tr key={col.name}>
              <td>{col.name}</td>
              <td>{col.type}</td>
              <td>{col.not_null ? t("data_studio_yes") : t("data_studio_no")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ResultTable({ result }: { result: QueryResult }) {  return (
    <table className="result-table">
      <thead>
        <tr>
          {result.columns.map((col) => <th key={col}>{col}</th>)}
        </tr>
      </thead>
      <tbody>
        {result.rows.map((row, i) => (
          <tr key={i}>
            {row.map((cell, j) => <td key={j}>{JSON.stringify(cell)}</td>)}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function JSONDetail({ path, transport }: { path: string; transport: Transport }) {
  const t = useTranslate();
  const [content, setContent] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    try {
      setContent(await readJSONFile(transport, path));
      setError(null);
    } catch (err) {
      setError(String(err));
    }
  };

  return (
    <div>
      <h3>JSON: {path}</h3>
      <Button onClick={load}>{t("data_studio_load")}</Button>
      {error && <Notice tone="problem" boxed>{error}</Notice>}
      {content !== null && <pre className="json-viewer">{JSON.stringify(content, null, 2)}</pre>}
    </div>
  );
}

function MarkdownDetail({ path, transport }: { path: string; transport: Transport }) {
  const t = useTranslate();
  const [content, setContent] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = async () => {
    try {
      setContent(await readMarkdownFile(transport, path));
      setError(null);
    } catch (err) {
      setError(String(err));
    }
  };

  return (
    <div>
      <h3>Markdown: {path}</h3>
      <Button onClick={load}>{t("data_studio_load")}</Button>
      {error && <Notice tone="problem" boxed>{error}</Notice>}
      {content !== null && <pre className="markdown-viewer">{content}</pre>}
    </div>
  );
}
