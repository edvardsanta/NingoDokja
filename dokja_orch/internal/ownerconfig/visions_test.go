package ownerconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAMissingVisionsFileLeavesNingoWithoutAWorldview(t *testing.T) {
	visions, err := LoadVisions(filepath.Join(t.TempDir(), "visions.md"))
	if err != nil || visions != "" {
		t.Fatalf("expected no worldview and no error, got %q, %v", visions, err)
	}
}

func TestVisionsAreReadAsTrimmedText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "visions.md")
	if err := os.WriteFile(path, []byte("\n# Worldview\n- Merit over birth.\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	visions, err := LoadVisions(path)
	if err != nil {
		t.Fatal(err)
	}
	if visions != "# Worldview\n- Merit over birth." {
		t.Fatalf("unexpected visions %q", visions)
	}
}

func TestOversizedOrBinaryVisionsAreRefusedWithoutEchoingThem(t *testing.T) {
	for name, content := range map[string][]byte{
		"oversized": []byte(strings.Repeat("secret ", 2000)),
		"binary":    {0xff, 0xfe, 's', 'e', 'c', 'r', 'e', 't'},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "visions.md")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadVisions(path)
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("the error leaked file content: %v", err)
			}
		})
	}
}
