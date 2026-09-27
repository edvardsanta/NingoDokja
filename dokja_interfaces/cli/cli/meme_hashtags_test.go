package cli

import (
	"reflect"
	"testing"
)

func TestBuildHashtagSuggestPayloadWithAURL(t *testing.T) {
	payload, err := buildHashtagSuggestPayload([]string{"  https://x/a.png "}, "", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(payload, map[string]any{"url": "https://x/a.png"}) {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestBuildHashtagSuggestPayloadWithText(t *testing.T) {
	payload, err := buildHashtagSuggestPayload(nil, "  o tio pegou o pave  ", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(payload, map[string]any{"text": "o tio pegou o pave"}) {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestBuildHashtagSuggestPayloadIncludesMinScoreOnlyWhenSet(t *testing.T) {
	payload, err := buildHashtagSuggestPayload([]string{"https://x/a.png"}, "", 0.75, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload["min_score"] != 0.75 {
		t.Fatalf("expected min_score 0.75, got %#v", payload["min_score"])
	}

	payload, err = buildHashtagSuggestPayload([]string{"https://x/a.png"}, "", 0.75, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, has := payload["min_score"]; has {
		t.Fatalf("expected no min_score field when the flag was not set, got %#v", payload)
	}
}

func TestBuildHashtagSuggestPayloadRequiresAURLOrText(t *testing.T) {
	if _, err := buildHashtagSuggestPayload(nil, "", 0, false); err == nil {
		t.Fatal("expected an error when neither a url nor text is given")
	}
	if _, err := buildHashtagSuggestPayload([]string{"   "}, "  ", 0, false); err == nil {
		t.Fatal("expected an error when both are blank")
	}
}

func TestNewMemeHashtagCommandRegistersItsSubcommands(t *testing.T) {
	app := NewApp()
	command := app.newMemeHashtagCommand(nil) //nolint:staticcheck // RunE closures only read ctx when invoked
	names := map[string]bool{}
	for _, child := range command.Commands() {
		names[child.Name()] = true
	}
	for _, want := range []string{"tag", "suggest", "list", "untag"} {
		if !names[want] {
			t.Fatalf("expected a %q subcommand, got %v", want, names)
		}
	}
}
