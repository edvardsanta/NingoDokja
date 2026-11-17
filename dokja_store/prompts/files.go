// Package prompts reads operator-owned model instructions from private Markdown files.
package prompts

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"
)

const MaxBytes = 64 << 10

var validName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Error identifies configuration failures without retaining any prompt content.
type Error struct{ Name string }

func (e *Error) Error() string {
	return fmt.Sprintf("required model prompt is unavailable or invalid: %s", e.Name)
}

var configured struct {
	sync.RWMutex
	directory string
}

// SetDirectory selects an already trusted configuration directory, never event data.
// An empty directory restores discovery from the current checkout or container mount.
func SetDirectory(directory string) {
	configured.Lock()
	configured.directory = directory
	configured.Unlock()
}

// Directory uses the existing config mount in containers. Native processes discover
// the nearest checkout; worktree boundaries prevent falling into another checkout.
func Directory() string {
	configured.RLock()
	directory := configured.directory
	configured.RUnlock()
	if directory != "" {
		return directory
	}
	if stat, err := os.Stat("/run/dokja-config"); err == nil && stat.IsDir() {
		return "/run/dokja-config/prompts"
	}
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Join(".dokja", "prompts")
	}
	for current := cwd; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return filepath.Join(current, ".dokja", "prompts")
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return filepath.Join(cwd, ".dokja", "prompts")
}

// ReadAt never follows a prompt symlink or returns a partial/empty prompt. Files
// contain instructions, so only the operator's chosen directory may supply them.
func ReadAt(directory, name string) (string, error) {
	failure := &Error{Name: name}
	if !validName.MatchString(name) {
		return "", failure
	}
	path := filepath.Join(directory, name+".md")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return "", failure
	}
	file, err := os.Open(path)
	if err != nil {
		return "", failure
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil || len(data) > MaxBytes || !utf8.Valid(data) || strings.IndexByte(string(data), 0) >= 0 {
		return "", failure
	}
	text := string(data)
	if strings.TrimSpace(text) == "" {
		return "", failure
	}
	return text, nil
}
func Read(name string) (string, error) { return ReadAt(Directory(), name) }

// ReadForRoot respects the host Runtime's existing checkout selection. Explicit
// injection still takes precedence for isolated callers and tests.
func ReadForRoot(root, name string) (string, error) {
	configured.RLock()
	directory := configured.directory
	configured.RUnlock()
	if directory != "" {
		return ReadAt(directory, name)
	}
	if root == "" {
		return Read(name)
	}
	return ReadAt(filepath.Join(root, ".dokja", "prompts"), name)
}

// MustRead is used only behind startup preflight or an orchestration boundary
// that reports configuration errors. It has no embedded text or fallback prompt.
func MustRead(name string) string {
	text, err := Read(name)
	if err != nil {
		panic(err)
	}
	return text
}

// Recover converts a missing prompt into an ordinary operation error; unrelated
// programming panics remain visible. It never logs or returns prompt contents.
func Recover(err *error) {
	if value := recover(); value != nil {
		if configuration, ok := value.(*Error); ok {
			*err = configuration
			return
		}
		panic(value)
	}
}

// Validate checks a component's prompt inventory before inference starts.
func Validate(names ...string) error {
	var failures []error
	for _, name := range names {
		if _, err := Read(name); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
