package cli

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildBookFilePayloadIncludesBase64AndFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.md")
	if err := os.WriteFile(path, []byte("# hello\n\nworld"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	payload, err := buildBookFilePayload(path, "Notes", "study", "pt-BR", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if payload["format"] != "md" {
		t.Fatalf("expected md format, got %#v", payload["format"])
	}
	if payload["filename"] != "sample.md" {
		t.Fatalf("expected sample.md filename, got %#v", payload["filename"])
	}
	if payload["title"] != "Notes" {
		t.Fatalf("expected title override, got %#v", payload["title"])
	}
	encoded, _ := payload["resource_bytes_b64"].(string)
	if encoded == "" {
		t.Fatal("expected resource_bytes_b64")
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(decoded) != "# hello\n\nworld" {
		t.Fatalf("unexpected decoded content %q", string(decoded))
	}
	if payload["content"] != "# hello\n\nworld" {
		t.Fatalf("expected inline content for markdown, got %#v", payload["content"])
	}
}
