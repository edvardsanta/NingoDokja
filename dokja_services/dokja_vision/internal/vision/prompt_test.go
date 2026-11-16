package vision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptFromFileReadsAnOperatorOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vision_prompt.md")
	if err := os.WriteFile(path, []byte("\nLeia a imagem em espanhol. Preserve números.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt, err := PromptFromFile(path)
	if err != nil || prompt != "Leia a imagem em espanhol. Preserve números." {
		t.Fatal(prompt, err)
	}
}

func TestPromptFromFileRejectsMissingEmptyAndOversizedFiles(t *testing.T) {
	if _, err := PromptFromFile(""); err == nil {
		t.Fatal("empty path accepted")
	}
	dir := t.TempDir()
	if _, err := PromptFromFile(filepath.Join(dir, "missing.md")); err == nil {
		t.Fatal("missing file accepted")
	}
	empty := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PromptFromFile(empty); err == nil {
		t.Fatal("empty file accepted")
	}
	huge := filepath.Join(dir, "huge.md")
	if err := os.WriteFile(huge, make([]byte, MaxPromptFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PromptFromFile(huge); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatal("oversized file accepted", err)
	}
}
