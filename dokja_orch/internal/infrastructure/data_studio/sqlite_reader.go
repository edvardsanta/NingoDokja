package datastudio

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// SQLiteReader inspects SQLite databases without modifying them.
type SQLiteReader struct{}

// NewSQLiteReader creates a read-only SQLite inspector.
func NewSQLiteReader() *SQLiteReader {
	return &SQLiteReader{}
}

// TableInfo describes one object in a SQLite schema.
type TableInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	SQL    string `json:"sql,omitempty"`
}

// ColumnInfo describes a column.
type ColumnInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	NotNull     bool   `json:"not_null"`
	Default     any    `json:"default,omitempty"`
	PrimaryKey  bool   `json:"primary_key"`
}

// TableSchema gathers tables, views, indexes and triggers.
type TableSchema struct {
	Objects []TableInfo  `json:"objects"`
	Columns []ColumnInfo `json:"columns,omitempty"`
	Count   int64        `json:"count"`
}

// Open opens a SQLite database read-only.
// WAL mode is intentionally not forced: a read-only connection cannot create
// or verify the WAL file, which would make the open fail for databases that
// are not already in WAL mode.
func (r *SQLiteReader) Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Schema returns schema objects for the database.
func (r *SQLiteReader) Schema(db *sql.DB) (*TableSchema, error) {
	rows, err := db.Query(`SELECT name, type, sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite_master: %w", err)
	}
	defer rows.Close()

	objects := make([]TableInfo, 0)
	for rows.Next() {
		var o TableInfo
		var sqlRaw sql.NullString
		if err := rows.Scan(&o.Name, &o.Type, &sqlRaw); err != nil {
			return nil, err
		}
		o.SQL = sqlRaw.String
		objects = append(objects, o)
	}
	return &TableSchema{Objects: objects}, rows.Err()
}

// TableSchema returns the columns and row count for one table or view.
func (r *SQLiteReader) TableSchema(db *sql.DB, name string) (*TableSchema, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_xinfo(%q)", name))
	if err != nil {
		return nil, fmt.Errorf("table info: %w", err)
	}
	defer rows.Close()

	cols := make([]ColumnInfo, 0)
	for rows.Next() {
		var cid int
		var notNull, pk int
		var col ColumnInfo
		var dflt sql.NullString
		var hidden int
		if err := rows.Scan(&cid, &col.Name, &col.Type, &notNull, &dflt, &pk, &hidden); err != nil {
			return nil, err
		}
		col.NotNull = notNull != 0
		col.PrimaryKey = pk != 0
		if dflt.Valid {
			col.Default = dflt.String
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var count int64
	_ = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %q", name)).Scan(&count)

	return &TableSchema{Columns: cols, Count: count}, nil
}

// QueryResult is a paginated SELECT result.
type QueryResult struct {
	Columns []string   `json:"columns"`
	Rows    [][]any    `json:"rows"`
	Total   int64      `json:"total"`
}

// Query runs a read-only SELECT against the database.
func (r *SQLiteReader) Query(db *sql.DB, query string, limit, offset int) (*QueryResult, error) {
	// Reject any statement that is not a plain SELECT.
	normalized := normalizeSQL(query)
	if !isSelectOnly(normalized) {
		return nil, fmt.Errorf("only SELECT statements are allowed")
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	all := make([][]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}
		all = append(all, values)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	total := int64(len(all))
	start := offset
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) || limit <= 0 {
		end = len(all)
	}

	return &QueryResult{
		Columns: columns,
		Rows:    all[start:end],
		Total:   total,
	}, nil
}

// TableRows returns a paginated, ordered preview of a table. With a sort spec
// the rows order by that column; otherwise a stable rowid order keeps paging
// deterministic when rowid exists.
func (r *SQLiteReader) TableRows(db *sql.DB, table string, limit, offset int, sort *SortSpec) (*QueryResult, error) {
	query := fmt.Sprintf("SELECT * FROM %q", table)
	switch {
	case sort != nil:
		exists, err := r.columnExists(db, table, sort.Column)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("unknown sort column %q", sort.Column)
		}
		direction := "ASC"
		if sort.Desc {
			direction = "DESC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", quoteSQLiteIdent(sort.Column), direction)
	default:
		// Add a stable order only when rowid exists to keep pagination deterministic.
		var hasRowID bool
		if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info(%q) WHERE name = 'rowid'", table)).Scan(&hasRowID); err == nil && hasRowID {
			query += " ORDER BY rowid"
		}
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	return r.Query(db, query, 0, 0)
}

// columnExists reports whether the table has a column with that exact name.
func (r *SQLiteReader) columnExists(db *sql.DB, table, column string) (bool, error) {
	var count int
	err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_xinfo(%q) WHERE name = %q", table, column)).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check column: %w", err)
	}
	return count > 0, nil
}

// FileName returns the base name of the database path.
func (r *SQLiteReader) FileName(path string) string {
	return filepath.Base(path)
}
