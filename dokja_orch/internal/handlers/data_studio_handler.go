package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
func NewDataStudioHandler(baseDir string, postgresDSNs map[string]string) *DataStudioHandler {
	var conns []datastudio.PostgresConnection
	id := 0
	for name, dsn := range postgresDSNs {
		id++
		conns = append(conns, datastudio.PostgresConnection{
			ID:   fmt.Sprintf("postgres:%d", id),
			Name: name,
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
	case "datastudio.sqlite.query":
		return h.sqliteQuery(ctx, event.Payload)
	case "datastudio.postgres.schemas":
		return h.postgresSchemas(ctx, event.Payload)
	case "datastudio.postgres.tables":
		return h.postgresTables(ctx, event.Payload)
	case "datastudio.postgres.table":
		return h.postgresTable(ctx, event.Payload)
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
