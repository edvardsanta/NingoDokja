package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	datastudio "read_books/internal/infrastructure/data_studio"
	"read_books/internal/core"
)

// DataStudioSourceLister discovers sources the Data Studio can browse.
type DataStudioSourceLister interface {
	Scan(postgres []datastudio.PostgresConnection) ([]datastudio.Source, error)
	ValidatePath(path string) (string, error)
}

// DataStudioHandler answers administrative read requests from the Data Studio workspace.
// It never writes unless an explicit future action is added; the first version is read-only.
type DataStudioHandler struct {
	discovery      DataStudioSourceLister
	sqlite         *datastudio.SQLiteReader
	postgres       *datastudio.PostgresReader
	postgresConns  []datastudio.PostgresConnection
	baseDir        string
	maxTextBytes   int64
	maxJSONBytes   int64
}

// NewDataStudioHandler creates the handler. postgresDSNs maps a display name to a DSN.
// Names that share one DSN (different env vars pointing at the same server and
// database) collapse into a single connection, so the studio never lists the
// same schemas twice under two labels.
func NewDataStudioHandler(baseDir string, postgresDSNs map[string]string) *DataStudioHandler {
	namesByDSN := make(map[string][]string, len(postgresDSNs))
	for name := range postgresDSNs {
		namesByDSN[postgresDSNs[name]] = append(namesByDSN[postgresDSNs[name]], name)
	}
	dsns := make([]string, 0, len(namesByDSN))
	for dsn := range namesByDSN {
		dsns = append(dsns, dsn)
	}
	sort.Strings(dsns)
	var conns []datastudio.PostgresConnection
	for id, dsn := range dsns {
		names := namesByDSN[dsn]
		sort.Strings(names)
		conns = append(conns, datastudio.PostgresConnection{
			ID:   fmt.Sprintf("postgres:%d", id+1),
			Name: strings.Join(names, " · "),
			DSN:  dsn,
		})
	}
	return &DataStudioHandler{
		discovery:     datastudio.NewDiscovery(baseDir),
		sqlite:        datastudio.NewSQLiteReader(),
		postgres:      datastudio.NewPostgresReader(),
		postgresConns: conns,
		baseDir:       baseDir,
		maxTextBytes:  8 * 1024 * 1024,
		maxJSONBytes:  16 * 1024 * 1024,
	}
}

func (h *DataStudioHandler) Domain() core.Domain {
	return core.DomainDataStudio
}

func (h *DataStudioHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	switch event.Type {
	case "datastudio.sources.list":
		return h.listSources(ctx)
	case "datastudio.sqlite.schema":
		return h.sqliteSchema(ctx, event.Payload)
	case "datastudio.sqlite.table":
		return h.sqliteTable(ctx, event.Payload)
	case "datastudio.sqlite.rows":
		return h.sqliteRows(ctx, event.Payload)
	case "datastudio.sqlite.query":
		return h.sqliteQuery(ctx, event.Payload)
	case "datastudio.postgres.schemas":
		return h.postgresSchemas(ctx, event.Payload)
	case "datastudio.postgres.tables":
		return h.postgresTables(ctx, event.Payload)
	case "datastudio.postgres.table":
		return h.postgresTable(ctx, event.Payload)
	case "datastudio.postgres.rows":
		return h.postgresRows(ctx, event.Payload)
	case "datastudio.postgres.query":
		return h.postgresQuery(ctx, event.Payload)
	case "datastudio.json.read":
		return h.readJSON(event.Payload)
	case "datastudio.markdown.read":
		return h.readMarkdown(event.Payload)
	case "datastudio.diagnostics.run":
		return h.runDiagnostics(ctx)
	default:
		return nil, fmt.Errorf("unsupported data studio event: %s", event.Type)
	}
}

func (h *DataStudioHandler) listSources(ctx context.Context) (map[string]any, error) {
	sources, err := h.discovery.Scan(h.postgresConns)
	if err != nil {
		return nil, err
	}
	if sources == nil {
		sources = make([]datastudio.Source, 0)
	}
	return map[string]any{"sources": sources}, nil
}

func (h *DataStudioHandler) sqliteSchema(ctx context.Context, payload map[string]any) (map[string]any, error) {
	path, err := h.validPath(payload)
	if err != nil {
		return nil, err
	}
	db, err := h.sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	schema, err := h.sqlite.Schema(db)
	if err != nil {
		return nil, err
	}
	return map[string]any{"schema": schema}, nil
}

func (h *DataStudioHandler) sqliteTable(ctx context.Context, payload map[string]any) (map[string]any, error) {
	path, err := h.validPath(payload)
	if err != nil {
		return nil, err
	}
	table, ok := payload["table"].(string)
	if !ok || table == "" {
		return nil, fmt.Errorf("table name is required")
	}
	db, err := h.sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	schema, err := h.sqlite.TableSchema(db, table)
	if err != nil {
		return nil, err
	}
	return map[string]any{"schema": schema}, nil
}

// sqliteRows pages through one table or view without the screen writing SQL.
func (h *DataStudioHandler) sqliteRows(ctx context.Context, payload map[string]any) (map[string]any, error) {
	path, err := h.validPath(payload)
	if err != nil {
		return nil, err
	}
	table, ok := payload["table"].(string)
	if !ok || table == "" {
		return nil, fmt.Errorf("table name is required")
	}
	limit := intArg(payload["limit"], 100)
	offset := intArg(payload["offset"], 0)
	if limit < 1 || limit > 500 || offset < 0 {
		return nil, fmt.Errorf("limit must be 1..500 and offset non-negative")
	}
	sort, err := sortArg(payload)
	if err != nil {
		return nil, err
	}
	db, err := h.sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := h.sqlite.TableRows(db, table, limit, offset, sort)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": rows}, nil
}

func (h *DataStudioHandler) sqliteQuery(ctx context.Context, payload map[string]any) (map[string]any, error) {
	path, err := h.validPath(payload)
	if err != nil {
		return nil, err
	}
	query, ok := payload["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := intArg(payload["limit"], 100)
	offset := intArg(payload["offset"], 0)

	db, err := h.sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	result, err := h.sqlite.Query(db, query, limit, offset)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result}, nil
}

func (h *DataStudioHandler) postgresSchemas(ctx context.Context, payload map[string]any) (map[string]any, error) {
	dsn, err := h.postgresDSN(payload)
	if err != nil {
		return nil, err
	}
	db, err := h.postgres.Open(dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	schemas, err := h.postgres.ListSchemas(db)
	if err != nil {
		return nil, err
	}
	return map[string]any{"schemas": schemas}, nil
}

func (h *DataStudioHandler) postgresTables(ctx context.Context, payload map[string]any) (map[string]any, error) {
	dsn, err := h.postgresDSN(payload)
	if err != nil {
		return nil, err
	}
	schema, ok := payload["schema"].(string)
	if !ok || schema == "" {
		return nil, fmt.Errorf("schema is required")
	}
	db, err := h.postgres.Open(dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tables, err := h.postgres.ListTables(db, schema)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tables": tables}, nil
}

func (h *DataStudioHandler) postgresTable(ctx context.Context, payload map[string]any) (map[string]any, error) {
	dsn, err := h.postgresDSN(payload)
	if err != nil {
		return nil, err
	}
	schema, _ := payload["schema"].(string)
	table, _ := payload["table"].(string)
	if schema == "" || table == "" {
		return nil, fmt.Errorf("schema and table are required")
	}
	db, err := h.postgres.Open(dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	info, err := h.postgres.TableSchema(db, schema, table)
	if err != nil {
		return nil, err
	}
	return map[string]any{"schema": info}, nil
}

// postgresRows pages through one table or view without the screen writing SQL.
func (h *DataStudioHandler) postgresRows(ctx context.Context, payload map[string]any) (map[string]any, error) {
	dsn, err := h.postgresDSN(payload)
	if err != nil {
		return nil, err
	}
	schema, ok := payload["schema"].(string)
	if !ok || schema == "" {
		return nil, fmt.Errorf("schema is required")
	}
	table, ok := payload["table"].(string)
	if !ok || table == "" {
		return nil, fmt.Errorf("table name is required")
	}
	limit := intArg(payload["limit"], 100)
	offset := intArg(payload["offset"], 0)
	if limit < 1 || limit > 500 || offset < 0 {
		return nil, fmt.Errorf("limit must be 1..500 and offset non-negative")
	}
	sort, err := sortArg(payload)
	if err != nil {
		return nil, err
	}
	db, err := h.postgres.Open(dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := h.postgres.TableRows(ctx, db, schema, table, limit, offset, sort)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": rows}, nil
}

func (h *DataStudioHandler) postgresQuery(ctx context.Context, payload map[string]any) (map[string]any, error) {
	dsn, err := h.postgresDSN(payload)
	if err != nil {
		return nil, err
	}
	query, ok := payload["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query is required")
	}
	limit := intArg(payload["limit"], 100)
	offset := intArg(payload["offset"], 0)

	db, err := h.postgres.Open(dsn)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	result, err := h.postgres.Query(ctx, db, query, limit, offset)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result}, nil
}

func (h *DataStudioHandler) readJSON(payload map[string]any) (map[string]any, error) {
	path, err := h.validPath(payload)
	if err != nil {
		return nil, err
	}
	value, err := datastudio.ReadJSONFile(path, h.maxJSONBytes)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": value, "path": path}, nil
}

func (h *DataStudioHandler) readMarkdown(payload map[string]any) (map[string]any, error) {
	path, err := h.validPath(payload)
	if err != nil {
		return nil, err
	}
	content, err := datastudio.ReadTextFile(path, h.maxTextBytes)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": content, "path": path}, nil
}

func (h *DataStudioHandler) runDiagnostics(ctx context.Context) (map[string]any, error) {
	sources, err := h.discovery.Scan(h.postgresConns)
	if err != nil {
		return nil, err
	}

	issues := make([]map[string]any, 0)
	for _, src := range sources {
		switch src.Kind {
		case datastudio.KindSQLite:
			if _, err := os.Stat(src.Path); err != nil {
				issues = append(issues, map[string]any{
					"kind":  "sqlite",
					"name":  src.Name,
					"error": err.Error(),
				})
			}
		case datastudio.KindJSON:
			if _, err := datastudio.ReadJSONFile(src.Path, h.maxJSONBytes); err != nil {
				issues = append(issues, map[string]any{
					"kind":  "json",
					"name":  src.Name,
					"error": err.Error(),
				})
			}
		case datastudio.KindPostgres:
			db, err := h.postgres.Open(src.DSN)
			if err != nil {
				issues = append(issues, map[string]any{
					"kind":  "postgres",
					"name":  src.Name,
					"error": err.Error(),
				})
			} else {
				db.Close()
			}
		}
	}

	return map[string]any{
		"sources": len(sources),
		"issues":  issues,
	}, nil
}

func (h *DataStudioHandler) validPath(payload map[string]any) (string, error) {
	path, ok := payload["path"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("path is required")
	}
	// Allow absolute paths only when they are inside baseDir.
	validated, err := h.discovery.ValidatePath(path)
	if err != nil {
		// Fallback: resolve relative to baseDir.
		candidate := filepath.Join(h.baseDir, path)
		validated, err = h.discovery.ValidatePath(candidate)
		if err != nil {
			return "", err
		}
	}
	return validated, nil
}

func (h *DataStudioHandler) postgresDSN(payload map[string]any) (string, error) {
	if dsn, ok := payload["dsn"].(string); ok && dsn != "" {
		return dsn, nil
	}
	id, ok := payload["connection_id"].(string)
	if !ok || id == "" {
		return "", fmt.Errorf("dsn or connection_id is required")
	}
	for _, conn := range h.postgresConns {
		if conn.ID == id {
			return conn.DSN, nil
		}
	}
	return "", fmt.Errorf("unknown postgres connection")
}

func intArg(v any, fallback int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return fallback
	}
}

// sortArg reads the optional sort_column/sort_dir pair from a rows payload.
// The column is only a name here; the readers validate it against the live
// schema before it reaches SQL.
func sortArg(payload map[string]any) (*datastudio.SortSpec, error) {
	column, _ := payload["sort_column"].(string)
	if column == "" {
		return nil, nil
	}
	desc := false
	if dir, ok := payload["sort_dir"].(string); ok && dir != "" {
		switch strings.ToLower(dir) {
		case "asc":
		case "desc":
			desc = true
		default:
			return nil, fmt.Errorf("sort_dir must be asc or desc")
		}
	}
	return &datastudio.SortSpec{Column: column, Desc: desc}, nil
}
