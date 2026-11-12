package datastudio

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresReader inspects PostgreSQL databases read-only.
type PostgresReader struct{}

// NewPostgresReader creates a PostgreSQL inspector.
func NewPostgresReader() *PostgresReader {
	return &PostgresReader{}
}

// PostgresSchema describes a namespace and its tables.
type PostgresSchema struct {
	Databases []string     `json:"databases,omitempty"`
	Schemas   []string     `json:"schemas,omitempty"`
	Tables    []TableInfo  `json:"tables"`
	Columns   []ColumnInfo `json:"columns,omitempty"`
	Count     int64        `json:"count,omitempty"`
}

// Open connects with a timeout and read-only intent.
func (r *PostgresReader) Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres ping failed")
	}

	// Best-effort read-only session.
	_, _ = db.ExecContext(context.Background(), "SET default_transaction_read_only = on")
	return db, nil
}

// ListSchemas returns non-system schemas in the current database.
func (r *PostgresReader) ListSchemas(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast') ORDER BY schema_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		schemas = append(schemas, s)
	}
	return schemas, rows.Err()
}

// ListTables returns tables and views inside a schema.
func (r *PostgresReader) ListTables(db *sql.DB, schema string) ([]TableInfo, error) {
	query := `SELECT table_name, table_type FROM information_schema.tables WHERE table_schema = $1 ORDER BY table_name`
	rows, err := db.Query(query, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		var typ string
		if err := rows.Scan(&t.Name, &typ); err != nil {
			return nil, err
		}
		switch typ {
		case "BASE TABLE":
			t.Type = "table"
		case "VIEW":
			t.Type = "view"
		default:
			t.Type = strings.ToLower(typ)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// TableSchema returns column metadata and row count for a table.
func (r *PostgresReader) TableSchema(db *sql.DB, schema, table string) (*PostgresSchema, error) {
	rows, err := db.Query(`SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		var nullable string
		var dflt sql.NullString
		if err := rows.Scan(&col.Name, &col.Type, &nullable, &dflt); err != nil {
			return nil, err
		}
		col.NotNull = nullable == "NO"
		if dflt.Valid {
			col.Default = dflt.String
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var count int64
	_ = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", quoteIdent(schema), quoteIdent(table))).Scan(&count)

	return &PostgresSchema{Columns: cols, Count: count}, nil
}

// TableRows returns one page of a table or view, ordered by the primary key
// when one exists so paging stays deterministic.
func (r *PostgresReader) TableRows(ctx context.Context, db *sql.DB, schema, table string, limit, offset int) (*QueryResult, error) {
	query := fmt.Sprintf("SELECT * FROM %s.%s", quoteIdent(schema), quoteIdent(table))
	rows, err := db.Query(`SELECT a.attname FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey) WHERE i.indrelid = $1::regclass AND i.indisprimary ORDER BY a.attnum`, fmt.Sprintf("%s.%s", schema, table))
	if err == nil {
		var cols []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err == nil {
				cols = append(cols, quoteIdent(c))
			}
		}
		rows.Close()
		if len(cols) > 0 {
			query += " ORDER BY " + strings.Join(cols, ", ")
		}
	}
	return r.Query(ctx, db, query, limit, offset)
}

// Query runs a SELECT with a timeout and row limit.
func (r *PostgresReader) Query(ctx context.Context, db *sql.DB, query string, limit, offset int) (*QueryResult, error) {
	normalized := normalizeSQL(query)
	if !isSelectOnly(normalized) {
		return nil, fmt.Errorf("only SELECT statements are allowed")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	rows, err := db.QueryContext(ctx, query+fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset))
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var result [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &QueryResult{
		Columns: columns,
		Rows:    result,
		Total:   int64(len(result)),
	}, nil
}

func quoteIdent(name string) string {
	return "\"" + strings.ReplaceAll(name, "\"", "\"\"") + "\""
}
