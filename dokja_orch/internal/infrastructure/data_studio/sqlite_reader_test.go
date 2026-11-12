package datastudio

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	_ "modernc.org/sqlite"
)

func createSQLiteDB(path, schema string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(schema)
	return err
}

func TestSQLiteReaderReadsSchemaAndRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	if err := createSQLiteDB(path, `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT 'anon');
		CREATE INDEX idx_name ON users(name);
		INSERT INTO users (name) VALUES ('Ada'), ('Bob');
	`); err != nil {
		t.Fatalf("create db: %v", err)
	}

	r := NewSQLiteReader()
	db, err := r.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schema, err := r.Schema(db)
	if err != nil {
		t.Fatal(err)
	}

	var tableCount int
	for _, o := range schema.Objects {
		if o.Type == "table" || o.Type == "index" {
			tableCount++
		}
	}
	if tableCount < 2 {
		t.Fatalf("expected table and index, got %+v", schema.Objects)
	}

	ts, err := r.TableSchema(db, "users")
	if err != nil {
		t.Fatal(err)
	}
	if ts.Count != 2 {
		t.Errorf("expected 2 rows, got %d", ts.Count)
	}
	if len(ts.Columns) != 2 {
		t.Errorf("expected 2 columns, got %d", len(ts.Columns))
	}

	rows, err := r.TableRows(db, "users", 10, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Rows) != 2 {
		t.Errorf("expected 2 data rows, got %d", len(rows.Rows))
	}
}

func TestSQLiteReaderSortsRowsByColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sorted.db")
	if err := createSQLiteDB(path, `
		CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO users (name) VALUES ('Bob'), ('Ada'), ('Cid');
	`); err != nil {
		t.Fatalf("create db: %v", err)
	}

	r := NewSQLiteReader()
	db, err := r.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	asc, err := r.TableRows(db, "users", 10, 0, &SortSpec{Column: "name"})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowsColumn(asc, 1); !reflect.DeepEqual(got, []any{"Ada", "Bob", "Cid"}) {
		t.Errorf("asc by name: %v", got)
	}

	desc, err := r.TableRows(db, "users", 10, 0, &SortSpec{Column: "name", Desc: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := rowsColumn(desc, 1); !reflect.DeepEqual(got, []any{"Cid", "Bob", "Ada"}) {
		t.Errorf("desc by name: %v", got)
	}

	if _, err := r.TableRows(db, "users", 10, 0, &SortSpec{Column: "missing"}); err == nil {
		t.Error("sorting by an unknown column should fail")
	}
	if _, err := r.TableRows(db, "users", 10, 0, &SortSpec{Column: `name" LIMIT 1; --`}); err == nil {
		t.Error("a crafted column name should fail validation, not reach SQL")
	}
}

func rowsColumn(result *QueryResult, index int) []any {
	values := make([]any, 0, len(result.Rows))
	for _, row := range result.Rows {
		values = append(values, row[index])
	}
	return values
}

func TestSQLiteReaderRejectsWriteQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readonly.db")
	if err := createSQLiteDB(path, `CREATE TABLE t (id INTEGER PRIMARY KEY);`); err != nil {
		t.Fatalf("create db: %v", err)
	}

	r := NewSQLiteReader()
	db, err := r.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := r.Query(db, "INSERT INTO t VALUES (1)", 10, 0); err == nil {
		t.Error("INSERT should be rejected")
	}
	if _, err := r.Query(db, "SELECT 1; DROP TABLE t", 10, 0); err == nil {
		t.Error("multi-statement should be rejected")
	}
}

func TestSQLiteReaderDoesNotModifyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "immutable.db")
	if err := createSQLiteDB(path, `CREATE TABLE t (id INTEGER PRIMARY KEY); INSERT INTO t VALUES (1);`); err != nil {
		t.Fatalf("create db: %v", err)
	}

	infoBefore, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	r := NewSQLiteReader()
	db, err := r.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := r.Query(db, "SELECT * FROM t", 10, 0); err != nil {
		t.Fatal(err)
	}

	infoAfter, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if infoAfter.ModTime().After(infoBefore.ModTime()) || infoAfter.Size() != infoBefore.Size() {
		t.Error("database file changed after read-only access")
	}
}
