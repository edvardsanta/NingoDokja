package runtime

import (
	"context"
	"strings"
	"testing"
)

func previews(provider string, chunks ...string) []string {
	var seen []string
	p := &chatPreview{provider: provider, emit: func(text string) { seen = append(seen, text) }}
	for _, chunk := range chunks {
		p.observe([]byte(chunk))
	}
	return seen
}

func TestOllamaPreviewIsTheTextSoFarAndWaitsForACutCharacter(t *testing.T) {
	ç := "ç"
	seen := previews("ollama", "Ol", "á, "+ç[:1], ç[1:]+"a")
	if strings.Join(seen, "|") != "Ol|Olá, |Olá, ça" {
		t.Fatalf("%q", seen)
	}
}

func TestCodexPreviewReadsAgentMessagesAcrossWrites(t *testing.T) {
	seen := previews("codex",
		`{"type":"thread.started"}`+"\n"+`{"type":"item.updated","item":{"type":"agent_message","text":"The"}}`,
		"\n"+`{"type":"item.started","item":{"type":"command_execution","text":"rm -rf"}}`+"\n",
		`{"type":"item.completed","item":{"type":"agent_message","text":"The answer"}}`+"\n",
	)
	if strings.Join(seen, "|") != "The|The answer" {
		t.Fatalf("%q", seen)
	}
}

func TestPreviewIgnoresWhatItCannotRead(t *testing.T) {
	if seen := previews("codex", "not json\n", `{"type":"item.completed","item":{"type":"reasoning","text":"thinking"}}`+"\n"); len(seen) != 0 {
		t.Fatalf("%q", seen)
	}
	if seen := previews("ollama", "   "); len(seen) != 0 {
		t.Fatalf("%q", seen)
	}
}

func TestRunObservedSeesOutputAsTheProcessWritesIt(t *testing.T) {
	var seen []string
	out, err := ChatAdapters{}.runObserved(context.Background(), "sh", []string{"-c", "printf first; sleep 0.2; printf ' second'"}, "", t.TempDir(), func(chunk []byte) { seen = append(seen, string(chunk)) })
	if err != nil || string(out) != "first second" {
		t.Fatal(string(out), err)
	}
	if len(seen) < 2 || seen[0] != "first" {
		t.Fatalf("output was not observed as it came: %q", seen)
	}
}
