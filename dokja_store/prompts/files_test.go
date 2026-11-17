package prompts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPrivatePromptPreservesBytesAndReloadsEdits(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "example.md")
	for _, text := range []string{"fixture %s\n", "  updated fixture  "} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadAt(directory, "example")
		if err != nil || got != text {
			t.Fatal("private prompt not loaded verbatim", err)
		}
	}
}

func TestReadRejectsInvalidFilesWithoutExposingContents(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing", ""}, {"empty", ""}, {"blank", " \n\t"},
		{"oversized", strings.Repeat("x", MaxBytes+1)},
		{"invalid_utf", string([]byte{0xff})}, {"nul", "private\x00marker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			if tc.name != "missing" {
				if err := os.WriteFile(filepath.Join(directory, "example.md"), []byte(tc.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			text, err := ReadAt(directory, "example")
			var configuration *Error
			if text != "" || !errors.As(err, &configuration) || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid prompt was accepted or exposed")
			}
		})
	}
	for _, name := range []string{"../secret", "/secret", "example.md", "other/file", ""} {
		if _, err := ReadAt(t.TempDir(), name); err == nil {
			t.Fatal("untrusted filename accepted")
		}
	}
}

func TestReadRejectsSymlinksAndDirectories(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "folder.md"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "real.md"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", filepath.Join(directory, "link.md")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"folder", "link"} {
		if _, err := ReadAt(directory, name); err == nil {
			t.Fatal("non-regular prompt accepted")
		}
	}
}

func TestDiscoveryStopsAtNearestWorktree(t *testing.T) {
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	nested := filepath.Join(checkout, "module", "package")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".git"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if got := Directory(); got != filepath.Join(checkout, ".dokja", "prompts") {
		t.Fatalf("wrong checkout: %s", got)
	}
}

func TestMissingPromptIsAnOperationErrorAndOtherPanicsPropagate(t *testing.T) {
	SetDirectory(t.TempDir())
	t.Cleanup(func() { SetDirectory("") })
	operation := func() (failure error) { defer Recover(&failure); MustRead("missing"); return nil }
	var configuration *Error
	if err := operation(); !errors.As(err, &configuration) {
		t.Fatal("configuration panic escaped the boundary")
	}
	defer func() {
		if recover() != "programming error" {
			t.Fatal("unrelated panic was swallowed")
		}
	}()
	func() { var failure error; defer Recover(&failure); panic("programming error") }()
}

func TestRuntimeReadsItsSelectedCheckout(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, ".dokja", "prompts")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "example.md"), []byte("selected checkout fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := ReadForRoot(root, "example")
	if err != nil || text != "selected checkout fixture" {
		t.Fatal("Runtime ignored its checkout", err)
	}
}

func TestPromptInventoryHasDistinctSafeNames(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range Names {
		if seen[name] || !validName.MatchString(name) {
			t.Fatalf("ambiguous prompt identifier: %s", name)
		}
		seen[name] = true
	}
}
