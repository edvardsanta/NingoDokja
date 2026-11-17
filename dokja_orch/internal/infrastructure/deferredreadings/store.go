// Package deferredreadings persists promised deferred-reading follow-ups so an
// orchestrator restart re-arms them instead of dropping the promise. It stores a
// single bounded JSON document, replaced atomically; it never logs contents.
package deferredreadings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// maxBytes bounds the persisted document; promises carry no page content, so the
// bound is generous for metadata alone.
const maxBytes = 1 << 20

type Store struct{ path string }

func PathBeside(sharedDB string) string {
	return PathFor(sharedDB, "dokja_deferred_readings.json")
}

// PathFor places one named snapshot document beside the runtime databases, the
// same directory the other local stores use.
func PathFor(sharedDB, filename string) string {
	return filepath.Join(filepath.Dir(sharedDB), filename)
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("deferred readings path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create deferred readings directory: %w", err)
	}
	return &Store{path: path}, nil
}

// Load returns the persisted document, or found=false when none exists.
func (s *Store) Load(_ context.Context) (data []byte, found bool, err error) {
	if s == nil || s.path == "" {
		return nil, false, nil
	}
	content, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read deferred readings: %w", err)
	}
	if len(content) > maxBytes {
		return nil, false, fmt.Errorf("deferred readings document exceeds the bound")
	}
	return content, true, nil
}

// Save replaces the document atomically; an empty document removes it.
func (s *Store) Save(_ context.Context, data []byte) error {
	if s == nil || s.path == "" {
		return nil
	}
	if len(data) > maxBytes {
		return fmt.Errorf("deferred readings document exceeds the bound")
	}
	if len(data) == 0 {
		if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove deferred readings: %w", err)
		}
		return nil
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return fmt.Errorf("write deferred readings: %w", err)
	}
	if err := os.Rename(temporary, s.path); err != nil {
		return fmt.Errorf("replace deferred readings: %w", err)
	}
	return nil
}
