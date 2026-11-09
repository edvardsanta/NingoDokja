package ownerconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAMissingWarmFileLeavesTheUsualVoice(t *testing.T) {
	warm, err := LoadWarm(filepath.Join(t.TempDir(), "warm.md"))
	if err != nil || warm != "" {
		t.Fatalf("expected no text and no error, got %q, %v", warm, err)
	}
}

func TestTheWarmFileIsReadAsTrimmedTextAndOversizedIsRefusedWithoutEchoingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warm.md")
	if err := os.WriteFile(path, []byte("\nGentle, still playful.\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if warm, err := LoadWarm(path); err != nil || warm != "Gentle, still playful." {
		t.Fatalf("unexpected %q, %v", warm, err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("secret ", 2000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWarm(path); err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "warm file") {
		t.Fatalf("expected a refusal naming the file kind only, got %v", err)
	}
}
