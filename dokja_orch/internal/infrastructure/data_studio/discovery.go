// Package datastudio provides read-only administrative access to the files and databases
// Ningo uses, exposed through the orchestrator so interfaces never touch storage directly.
package datastudio

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourceKind classifies a discovered data source.
type SourceKind string

const (
	KindSQLite    SourceKind = "sqlite"
	KindPostgres  SourceKind = "postgres"
	KindJSON      SourceKind = "json"
	KindMarkdown  SourceKind = "markdown"
	KindPersona   SourceKind = "persona"
)

// Source is a discovered resource the Data Studio can open.
type Source struct {
	ID       string     `json:"id"`
	Kind     SourceKind `json:"kind"`
	Name     string     `json:"name"`
	Path     string     `json:"path,omitempty"`
	DSN      string     `json:"dsn,omitempty"`
	Database string     `json:"database,omitempty"`
	Schema   string     `json:"schema,omitempty"`
	Size     int64      `json:"size"`
	Error    string     `json:"error,omitempty"`
}

// PostgresConnection is a configured PostgreSQL endpoint.
type PostgresConnection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DSN      string `json:"dsn"`
	Database string `json:"database"`
}

// Discovery scans the operator's data directory for known sources.
type Discovery struct {
	baseDir string
}

// NewDiscovery creates a scanner rooted at baseDir (typically the checkout directory).
func NewDiscovery(baseDir string) *Discovery {
	return &Discovery{baseDir: baseDir}
}

// Scan returns SQLite files, JSON configs, Markdown docs and configured PostgreSQL connections.
func (d *Discovery) Scan(postgres []PostgresConnection) ([]Source, error) {
	var sources []Source

	sqlite, err := d.scanSQLite()
	if err != nil {
		return nil, err
	}
	sources = append(sources, sqlite...)

	jsons, err := d.scanJSON()
	if err != nil {
		return nil, err
	}
	sources = append(sources, jsons...)

	mds, err := d.scanMarkdown()
	if err != nil {
		return nil, err
	}
	sources = append(sources, mds...)

	for _, conn := range postgres {
		sources = append(sources, Source{
			ID:       conn.ID,
			Kind:     KindPostgres,
			Name:     conn.Name,
			DSN:      conn.DSN,
			Database: conn.Database,
		})
	}

	sortSources(sources)
	return sources, nil
}

func (d *Discovery) scanSQLite() ([]Source, error) {
	dotDokja := filepath.Join(d.baseDir, ".dokja")
	var sources []Source

	for _, rel := range []string{
		".",
		".dokja",
		"dokja_orch",
		"dokja_services/dokja_knowledge",
		"dokja_interfaces/discord/data",
		"dokja_lab",
	} {
		dir := filepath.Join(d.baseDir, rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !isSQLiteFile(name) {
				continue
			}
			path := filepath.Join(dir, name)
			info, err := entry.Info()
			if err != nil {
				continue
			}
			sources = append(sources, Source{
				ID:   "sqlite:" + path,
				Kind: KindSQLite,
				Name: name,
				Path: path,
				Size: info.Size(),
			})
		}
	}

	// Also walk .dokja for any other .db/.sqlite files (backups excluded).
	_ = filepath.WalkDir(dotDokja, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !isSQLiteFile(name) {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"backups"+string(filepath.Separator)) ||
			strings.Contains(path, string(filepath.Separator)+"backup"+string(filepath.Separator)) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// Avoid duplicates from the explicit scan above.
		for _, s := range sources {
			if s.Path == path {
				return nil
			}
		}
		sources = append(sources, Source{
			ID:   "sqlite:" + path,
			Kind: KindSQLite,
			Name: name,
			Path: path,
			Size: info.Size(),
		})
		return nil
	})

	return sources, nil
}

func isSQLiteFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".db" || ext == ".sqlite" || ext == ".sqlite3"
}

func (d *Discovery) scanJSON() ([]Source, error) {
	var sources []Source
	for _, rel := range []string{
		".dokja/owner.json",
		".dokja/state.json",
		".dokja/post-reader.json",
		".dokja/macro-context.compose.json",
		".dokja/host-workers.json",
		"dokja_domain/dokja_chat/capabilities.json",
		"dokja_orch/owner.example.json",
		"steps.json",
	} {
		path := filepath.Join(d.baseDir, rel)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		sources = append(sources, Source{
			ID:   "json:" + path,
			Kind: KindJSON,
			Name: filepath.Base(path),
			Path: path,
			Size: info.Size(),
		})
	}
	return sources, nil
}

func (d *Discovery) scanMarkdown() ([]Source, error) {
	var sources []Source
	for _, rel := range []string{
		".dokja/visions.md",
		".dokja/warm.md",
		"AGENTS.md",
		"CLAUDE.md",
		"README.md",
		"dokja_docs/production-runbook.md",
		"dokja_docs/capability-map.md",
		"dokja_docs/desktop-app-plan.md",
	} {
		path := filepath.Join(d.baseDir, rel)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		kind := KindMarkdown
		if rel == ".dokja/visions.md" || rel == ".dokja/warm.md" {
			kind = KindPersona
		}
		sources = append(sources, Source{
			ID:   string(kind) + ":" + path,
			Kind: kind,
			Name: filepath.Base(path),
			Path: path,
			Size: info.Size(),
		})
	}
	return sources, nil
}

func sortSources(sources []Source) {
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Kind != sources[j].Kind {
			return sources[i].Kind < sources[j].Kind
		}
		return strings.ToLower(sources[i].Name) < strings.ToLower(sources[j].Name)
	})
}

// ValidatePath ensures a requested path stays inside baseDir and is not a symlink escape.
func (d *Discovery) ValidatePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	base, err := filepath.Abs(d.baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base dir: %w", err)
	}
	if !strings.HasPrefix(abs, base+string(filepath.Separator)) && abs != base {
		return "", fmt.Errorf("path is outside the configured data directory")
	}
	return abs, nil
}
