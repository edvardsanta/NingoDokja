package datastudio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsSelectOnly(t *testing.T) {
	cases := []struct {
		query string
		ok    bool
	}{
		{"SELECT 1", true},
		{"select * from users", true},
		{"  SELECT id FROM t", true},
		{"INSERT INTO t VALUES (1)", false},
		{"SELECT 1; DROP TABLE t", false},
		{"UPDATE t SET x=1", false},
		{"DELETE FROM t", false},
	}

	for _, c := range cases {
		got := isSelectOnly(normalizeSQL(c.query))
		if got != c.ok {
			t.Errorf("isSelectOnly(%q) = %v, want %v", c.query, got, c.ok)
		}
	}
}

func TestReadTextFileCapsSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0640); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadTextFile(path, 5); err == nil {
		t.Error("expected oversized file to be rejected")
	}

	got, err := ReadTextFile(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestReadJSONFileParsesAndRejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0640); err != nil {
		t.Fatal(err)
	}

	val, err := ReadJSONFile(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := val.(map[string]any)
	if !ok || m["ok"] != true {
		t.Errorf("unexpected parsed value: %v", val)
	}

	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte(`{not json}`), 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadJSONFile(badPath, 100); err == nil {
		t.Error("expected invalid JSON to be rejected")
	}
}
