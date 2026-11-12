package datastudio

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// SortSpec orders table rows by one column. Readers validate the column
// against the live schema before it reaches SQL, so the screen never
// interpolates an identifier unchecked.
type SortSpec struct {
	Column string
	Desc   bool
}

func normalizeSQL(q string) string {
	return strings.ToLower(strings.TrimSpace(q))
}

// quoteSQLiteIdent escapes an identifier for SQLite double quoting.
func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func isSelectOnly(q string) bool {
	// Simple allowlist: the statement must start with SELECT and contain no semicolons
	// (which could terminate one statement and start another).
	if !strings.HasPrefix(q, "select") {
		return false
	}
	return !strings.Contains(q, ";")
}

// ReadTextFile reads a UTF-8 text file, capping size to avoid abuse.
func ReadTextFile(path string, maxBytes int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxBytes)
	}

	data := make([]byte, info.Size())
	_, err = file.Read(data)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadJSONFile reads and parses a JSON file.
func ReadJSONFile(path string, maxBytes int64) (any, error) {
	raw, err := ReadTextFile(path, maxBytes)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	return value, nil
}
