package digest

import (
	"context"
	"fmt"
	"testing"
)

type webService struct {
	kind    string
	payload map[string]any
}

func (s *webService) Dispatch(_ context.Context, kind string, payload map[string]any) (map[string]any, error) {
	s.kind, s.payload = kind, payload
	return map[string]any{"title": "Page"}, nil
}
func TestWebReadingUsesNonExecutingReader(t *testing.T) {
	s := &webService{}
	d := New(s)
	_, err := d.Handle(context.Background(), Request{Action: ActionReadWebLink, Event: Event{Payload: map[string]any{"url": "https://example.com/law#art140"}}})
	if err != nil || s.kind != "feeds.web.read" || s.payload["url"] != "https://example.com/law#art140" {
		t.Fatalf("%#v %v", s, err)
	}
	s.kind = ""
	if _, err := d.ReadWebLink(context.Background(), "file:///etc/passwd"); err == nil || s.kind != "" {
		t.Fatal("invalid address reached service")
	}
}

func TestWebFailureReasonsExposeOnlyFixedCategories(t *testing.T) {
	for message, want := range map[string]string{"web_blocked: secret challenge": "blocked", "web_timeout: internal details": "timeout", "web_pdf_unreadable: parser details": "pdf_unreadable", "private transport diagnostic": "unavailable"} {
		if got := WebFailureReason(fmt.Errorf("%s", message)); got != want {
			t.Fatalf("%s", got)
		}
	}
}
