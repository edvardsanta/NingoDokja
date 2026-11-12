import type { Transport, TransportResult } from "../../shared/transport.js";

export type SourceKind = "sqlite" | "postgres" | "json" | "markdown" | "persona";

export type Source = {
  id: string;
  kind: SourceKind;
  name: string;
  path?: string;
  dsn?: string;
  database?: string;
  schema?: string;
  size: number;
  error?: string;
};

export type SQLiteSchema = {
  objects: { name: string; type: string; sql?: string }[];
};

export type TableSchema = {
  columns: { name: string; type: string; not_null: boolean; default?: unknown; primary_key: boolean }[];
  count: number;
};

export type QueryResult = {
  columns: string[];
  rows: unknown[][];
  total: number;
};

export type Diagnostics = {
  sources: number;
  issues: { kind: SourceKind; name: string; error: string }[];
};

export async function listSources(transport: Transport): Promise<Source[]> {
  const reply = await transport.request("datastudio.sources.list", {});
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { sources: Source[] } | undefined;
  return result?.sources ?? [];
}

export async function readSQLiteSchema(transport: Transport, path: string): Promise<SQLiteSchema> {
  const reply = await transport.request("datastudio.sqlite.schema", { path });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { schema: SQLiteSchema } | undefined;
  return result?.schema ?? { objects: [] };
}

export async function readSQLiteTable(transport: Transport, path: string, table: string): Promise<TableSchema> {
  const reply = await transport.request("datastudio.sqlite.table", { path, table });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { schema: TableSchema } | undefined;
  return result?.schema ?? { columns: [], count: 0 };
}

export async function readSQLiteRows(transport: Transport, path: string, table: string, limit = 100, offset = 0): Promise<QueryResult> {
  const reply = await transport.request("datastudio.sqlite.rows", { path, table, limit, offset });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { result: QueryResult } | undefined;
  return result?.result ?? { columns: [], rows: [], total: 0 };
}

export async function querySQLite(transport: Transport, path: string, query: string, limit = 100, offset = 0): Promise<QueryResult> {
  const reply = await transport.request("datastudio.sqlite.query", { path, query, limit, offset });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { result: QueryResult } | undefined;
  return result?.result ?? { columns: [], rows: [], total: 0 };
}

export async function readJSONFile(transport: Transport, path: string): Promise<unknown> {
  const reply = await transport.request("datastudio.json.read", { path });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { content: unknown } | undefined;
  return result?.content;
}

export async function readMarkdownFile(transport: Transport, path: string): Promise<string> {
  const reply = await transport.request("datastudio.markdown.read", { path });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { content: string } | undefined;
  return result?.content ?? "";
}

export async function runDiagnostics(transport: Transport): Promise<Diagnostics> {
  const reply = await transport.request("datastudio.diagnostics.run", {});
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as Diagnostics | undefined;
  return result ?? { sources: 0, issues: [] };
}

export async function listPostgresSchemas(transport: Transport, id: string): Promise<string[]> {
  const reply = await transport.request("datastudio.postgres.schemas", { connection_id: id });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { schemas: string[] } | undefined;
  return result?.schemas ?? [];
}

export async function listPostgresTables(transport: Transport, id: string, schema: string): Promise<{ name: string; type: string }[]> {
  const reply = await transport.request("datastudio.postgres.tables", { connection_id: id, schema });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { tables: { name: string; type: string }[] } | undefined;
  return result?.tables ?? [];
}

export async function readPostgresTable(transport: Transport, id: string, schema: string, table: string): Promise<TableSchema> {
  const reply = await transport.request("datastudio.postgres.table", { connection_id: id, schema, table });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { schema: TableSchema } | undefined;
  return result?.schema ?? { columns: [], count: 0 };
}

export async function readPostgresRows(transport: Transport, id: string, schema: string, table: string, limit = 100, offset = 0): Promise<QueryResult> {
  const reply = await transport.request("datastudio.postgres.rows", { connection_id: id, schema, table, limit, offset });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { result: QueryResult } | undefined;
  return result?.result ?? { columns: [], rows: [], total: 0 };
}

export async function queryPostgres(transport: Transport, id: string, query: string, limit = 100, offset = 0): Promise<QueryResult> {
  const reply = await transport.request("datastudio.postgres.query", { connection_id: id, query, limit, offset });
  if (!reply.ok) throw new Error(reply.error.message);
  const result = reply.result as { result: QueryResult } | undefined;
  return result?.result ?? { columns: [], rows: [], total: 0 };
}

// The screen never sees credentials. The shell masks the DSN to host:port/database
// before it reaches the renderer; this stays as the defense in depth for anything
// that slips through, and passes already-masked values straight on.
export function maskedDSN(dsn: string): string {
  if (!dsn.includes("://") && !dsn.includes("@")) return dsn;
  try {
    const url = new URL(dsn);
    const host = url.hostname + (url.port ? `:${url.port}` : "");
    const database = url.pathname.replace(/^\//, "");
    return database ? `${host}/${database}` : host;
  } catch {
    return "(configured)";
  }
}
