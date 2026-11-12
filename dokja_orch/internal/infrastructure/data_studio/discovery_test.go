package datastudio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoveryFindsSQLiteAndConfigFiles(t *testing.T) {
	base := t.TempDir()
	dotDokja := filepath.Join(base, ".dokja")
	if err := os.MkdirAll(dotDokja, 0750); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dotDokja, "ningo_memory.db")
	if err := createSQLiteDB(dbPath, `CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT);`); err != nil {
		t.Fatalf("create sqlite: %v", err)
	}

	jsonPath := filepath.Join(dotDokja, "owner.json")
	if err := os.WriteFile(jsonPath, []byte(`{"name":"test"}`), 0640); err != nil {
		t.Fatal(err)
	}

	mdPath := filepath.Join(base, "AGENTS.md")
	if err := os.WriteFile(mdPath, []byte("# agents"), 0640); err != nil {
		t.Fatal(err)
	}

	d := NewDiscovery(base)
	sources, err := d.Scan(nil)
	if err != nil {
		t.Fatal(err)
	}

	var foundSQLite, foundJSON, foundMD bool
	for _, s := range sources {
		switch s.Kind {
		case KindSQLite:
			if s.Name == "ningo_memory.db" {
				foundSQLite = true
			}
		case KindJSON:
			if s.Name == "owner.json" {
				foundJSON = true
			}
		case KindMarkdown:
			if s.Name == "AGENTS.md" {
				foundMD = true
			}
		}
	}

	if !foundSQLite {
		t.Error("expected SQLite source ningo_memory.db")
	}
	if !foundJSON {
		t.Error("expected JSON source owner.json")
	}
	if !foundMD {
		t.Error("expected Markdown source AGENTS.md")
	}
}

func TestDiscoveryMarksPersonaFiles(t *testing.T) {
	base := t.TempDir()
	dotDokja := filepath.Join(base, ".dokja")
	if err := os.MkdirAll(dotDokja, 0750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"visions.md", "warm.md"} {
		if err := os.WriteFile(filepath.Join(dotDokja, name), []byte("# persona"), 0640); err != nil {
			t.Fatal(err)
		}
	}

	d := NewDiscovery(base)
	sources, err := d.Scan(nil)
	if err != nil {
		t.Fatal(err)
	}

	var visions, warm bool
	for _, s := range sources {
		if s.Name == "visions.md" {
			visions = s.Kind == KindPersona
		}
		if s.Name == "warm.md" {
			warm = s.Kind == KindPersona
		}
	}
	if !visions {
		t.Error("visions.md should be classified as persona")
	}
	if !warm {
		t.Error("warm.md should be classified as persona")
	}
}

func TestDiscoveryIncludesPostgresConnections(t *testing.T) {
	base := t.TempDir()
	d := NewDiscovery(base)
	conn := PostgresConnection{ID: "pg:local", Name: "local", DSN: "postgres://x", Database: "dokja"}
	sources, err := d.Scan([]PostgresConnection{conn})
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, s := range sources {
		if s.Kind == KindPostgres && s.Name == "local" && s.DSN == "postgres://x" {
			found = true
		}
	}
	if !found {
		t.Error("expected configured Postgres source")
	}
}

func TestValidatePathRejectsEscapes(t *testing.T) {
	base := t.TempDir()
	d := NewDiscovery(base)

	if _, err := d.ValidatePath(filepath.Join(base, "ok.txt")); err != nil {
		t.Errorf("expected ok.txt to be valid: %v", err)
	}
	if _, err := d.ValidatePath(filepath.Join(base, "..", "outside.txt")); err == nil {
		t.Error("expected path outside base to be rejected")
	}
}
