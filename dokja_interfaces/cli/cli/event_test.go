package cli

import "testing"

func TestBuildEventDefaults(t *testing.T) {
	event := BuildEvent(EmitOptions{
		Type: "meme.status",
	})

	if event.EventID == "" {
		t.Fatal("expected event id")
	}
	if event.Source != "cli" {
		t.Fatalf("expected cli source, got %q", event.Source)
	}
	if event.Type != "meme.status" {
		t.Fatalf("expected meme.status type, got %q", event.Type)
	}
	if event.Payload == nil {
		t.Fatal("expected payload map")
	}
	if event.Context == nil {
		t.Fatal("expected context map")
	}
}
