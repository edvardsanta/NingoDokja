package digest

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// readerService answers feeds.status with the given providers and feeds.invoke with the reply
// of the named reader.
type readerService struct {
	providers []any
	replies   map[string]map[string]any
	failures  map[string]error
	asked     []string
}

func (s *readerService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if eventType == "feeds.status" {
		return map[string]any{"providers": s.providers}, nil
	}
	if payload["capability"] != SocialCapability || payload["action"] != "read" {
		return nil, errors.New("unexpected invoke")
	}
	reader, _ := payload["plugin"].(string)
	s.asked = append(s.asked, reader)
	if err := s.failures[reader]; err != nil {
		return nil, err
	}
	return s.replies[reader], nil
}

func provider(id string, enabled bool, provides ...any) map[string]any {
	return map[string]any{"id": id, "enabled": enabled, "provides": provides}
}

func readLinkWith(t *testing.T, service *readerService, link string) map[string]any {
	t.Helper()
	reply, err := New(service).Handle(context.Background(), Request{
		Action: ActionReadLink, Event: Event{Type: "digest.link.read", Payload: map[string]any{"url": link}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestReadLinkAsksEnabledReadersInIdOrderAndTakesTheFirstThatHandlesIt(t *testing.T) {
	service := &readerService{
		providers: []any{
			provider("reader-b", true, SocialCapability),
			provider("reader-a", true, SocialCapability),
			provider("reader-off", false, SocialCapability),
			provider("music", true, "audio.resolve.v1"),
		},
		replies: map[string]map[string]any{
			"reader-a": {"handled": false},
			"reader-b": {"handled": true, "post": map[string]any{
				"url": "https://example.com/p/1", "author": "  someone  ", "caption": "a caption",
				"published": "2026-10-01T10:00:00-03:00",
				"media": []any{
					map[string]any{"kind": "video", "url": "https://media.example/v.mp4", "thumbnail": "https://media.example/t.jpg"},
					map[string]any{"kind": "audio", "url": "https://media.example/a.mp3"},
					map[string]any{"kind": "image", "url": "javascript:alert(1)"},
				},
			}},
		},
	}
	reply := readLinkWith(t, service, "https://example.com/p/1")
	if strings.Join(service.asked, ",") != "reader-a,reader-b" {
		t.Fatalf("asked %v", service.asked)
	}
	post, _ := reply["post"].(map[string]any)
	if reply["status"] != "ok" || post["reader"] != "reader-b" || post["author"] != "someone" {
		t.Fatal(reply)
	}
	if post["published"] != "2026-10-01T13:00:00Z" {
		t.Fatal(post["published"])
	}
	media, _ := post["media"].([]map[string]any)
	if len(media) != 1 || media[0]["kind"] != "video" || media[0]["thumbnail"] != "https://media.example/t.jpg" {
		t.Fatal(media)
	}
}

func TestReadLinkGivesTheSamePostOneDocumentId(t *testing.T) {
	first, _ := postFrom(map[string]any{"url": "https://www.example.com/p/1/?utm_source=x"})
	second, _ := postFrom(map[string]any{"url": "https://example.com/p/1"})
	other, _ := postFrom(map[string]any{"url": "https://example.com/p/2"})
	if first["document_id"] != second["document_id"] || first["document_id"] == other["document_id"] {
		t.Fatal(first, second, other)
	}
	if !strings.HasPrefix(first["document_id"].(string), "post-") {
		t.Fatal(first["document_id"])
	}
}

func TestReadLinkTurnsARefusalIntoAFixedReason(t *testing.T) {
	for given, want := range map[string]string{
		RefusedLoginRequired:      RefusedLoginRequired,
		RefusedUnavailable:        RefusedUnavailable,
		"blocked by <b>host</b>!": RefusedFailed,
	} {
		service := &readerService{
			providers: []any{provider("reader-a", true, SocialCapability), provider("reader-b", true, SocialCapability)},
			replies:   map[string]map[string]any{"reader-a": {"handled": true, "refused": given}},
		}
		reply := readLinkWith(t, service, "https://example.com/p/1")
		if reply["status"] != "refused" || reply["refused"] != want || len(service.asked) != 1 {
			t.Fatal(given, reply, service.asked)
		}
	}
}

func TestReadLinkSaysWhyNothingWasShown(t *testing.T) {
	none := &readerService{providers: []any{provider("music", true, "audio.resolve.v1")}}
	if reply := readLinkWith(t, none, "https://example.com/p/1"); reply["refused"] != RefusedNoReader {
		t.Fatal(reply)
	}
	declined := &readerService{
		providers: []any{provider("reader-a", true, SocialCapability)},
		replies:   map[string]map[string]any{"reader-a": {"handled": false}},
	}
	if reply := readLinkWith(t, declined, "https://example.com/p/1"); reply["refused"] != RefusedUnsupported {
		t.Fatal(reply)
	}
	broken := &readerService{
		providers: []any{provider("reader-a", true, SocialCapability), provider("reader-b", true, SocialCapability)},
		failures:  map[string]error{"reader-a": errors.New("plugin crashed")},
		replies:   map[string]map[string]any{"reader-b": {"handled": true, "post": map[string]any{"caption": "no address"}}},
	}
	if reply := readLinkWith(t, broken, "https://example.com/p/1"); reply["refused"] != RefusedFailed {
		t.Fatal(reply)
	}
}

func TestReadLinkRefusesWhatIsNotAWebLink(t *testing.T) {
	for _, link := range []string{"", "ftp://example.com/x", "https://user:secret@example.com/p", "not a link", "https://example.com/" + strings.Repeat("a", maxLinkChars)} {
		service := &readerService{}
		_, err := New(service).Handle(context.Background(), Request{
			Action: ActionReadLink, Event: Event{Payload: map[string]any{"url": link}},
		})
		if err == nil || len(service.asked) != 0 {
			t.Fatalf("%q was read", link)
		}
	}
}

func TestReadLinkBoundsTheCaption(t *testing.T) {
	post, ok := postFrom(map[string]any{"url": "https://example.com/p/1", "caption": strings.Repeat("é", maxCaptionRunes+10)})
	if !ok || len([]rune(post["caption"].(string))) != maxCaptionRunes {
		t.Fatal(len([]rune(post["caption"].(string))))
	}
}
