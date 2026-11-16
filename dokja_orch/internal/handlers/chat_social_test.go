package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// socialReadingService answers the social reader dispatches and delegates web reading to
// the plain fake, so tests see which path served a link.
type socialReadingService struct {
	webReadingService
	readers    []any
	social     map[string]any
	socialErr  error
	invokeUrls []string
}

func (s *socialReadingService) Dispatch(ctx context.Context, kind string, payload map[string]any) (map[string]any, error) {
	switch kind {
	case "feeds.status":
		return map[string]any{"providers": s.readers}, nil
	case "feeds.invoke":
		input, _ := payload["input"].(map[string]any)
		s.invokeUrls = append(s.invokeUrls, fmt.Sprint(input["url"]))
		if s.socialErr != nil {
			return nil, s.socialErr
		}
		return s.social, nil
	default:
		return s.webReadingService.Dispatch(ctx, kind, payload)
	}
}

func socialReaderProvider() []any {
	return []any{map[string]any{"id": "social-posts", "enabled": true, "provides": []any{"social.resolve.v1"}}}
}

const reelURL = "https://www.instagram.example/reel/Dfs6JqmNkyn/"

func TestSocialPostReaderAnswersChatLinksBeforeTheWebReader(t *testing.T) {
	service := &socialReadingService{
		readers: socialReaderProvider(),
		social: map[string]any{"handled": true, "post": map[string]any{
			"url":       "https://www.instagram.example/p/Dfs6JqmNkyn/",
			"author":    "reelauthor",
			"caption":   "reels da madrugada",
			"published": "2026-11-14T00:22:55Z",
		}},
	}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "O reel do reelauthor. [1]"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("olha esse reel "+reelURL), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.calls) != 0 {
		t.Fatalf("the web reader ran though the post reader answered: %#v", service.calls)
	}
	if len(service.invokeUrls) != 1 || service.invokeUrls[0] != reelURL {
		t.Fatalf("the reader was not asked about the link: %#v", service.invokeUrls)
	}
	found := false
	for _, message := range generator.messages {
		if message["role"] == "user" && strings.Contains(message["content"], "UNTRUSTED_WEB_EVIDENCE") &&
			strings.Contains(message["content"], "reels da madrugada") && strings.Contains(message["content"], "reelauthor") {
			found = true
		}
	}
	if !found {
		t.Fatal("the post reading never reached the generator")
	}
}

func TestChatLinkFallsBackToWebReadingWhenPostReadersRefuse(t *testing.T) {
	for name, social := range map[string]map[string]any{
		"refused":    {"handled": true, "refused": "login_required"},
		"notHandled": {"handled": false},
	} {
		t.Run(name, func(t *testing.T) {
			service := &socialReadingService{readers: socialReaderProvider(), social: social}
			generator := &fakeChatGenerator{result: map[string]any{"reply": "so a parede de login [1]"}}
			h := NewChatDomainHandler(generator).WithWebReading(service, nil)
			if _, err := h.Handle(context.Background(), chatEvent(reelURL), chatStep); err != nil {
				t.Fatal(err)
			}
			if len(service.calls) != 1 {
				t.Fatalf("web reading must answer what the readers refuse: %#v", service.calls)
			}
		})
	}
}

func TestChatLinkFallsBackToWebReadingWithoutPostReaders(t *testing.T) {
	for name, readers := range map[string][]any{
		"noneEnabled": {map[string]any{"id": "social-posts", "enabled": false, "provides": []any{"social.resolve.v1"}}},
		"empty":       {},
	} {
		t.Run(name, func(t *testing.T) {
			service := &socialReadingService{readers: readers}
			generator := &fakeChatGenerator{result: map[string]any{"reply": "lei o artigo [1]"}}
			h := NewChatDomainHandler(generator).WithWebReading(service, nil)
			if _, err := h.Handle(context.Background(), chatEvent("https://example.com/law"), chatStep); err != nil {
				t.Fatal(err)
			}
			if len(service.calls) != 1 {
				t.Fatalf("web reading must serve links alone: %#v", service.calls)
			}
			if len(service.invokeUrls) != 0 {
				t.Fatalf("a disabled or missing reader must not be invoked: %#v", service.invokeUrls)
			}
		})
	}
}

func TestSocialPostWithoutAuthorOrCaptionFallsBackToWebReading(t *testing.T) {
	service := &socialReadingService{
		readers: socialReaderProvider(),
		social:  map[string]any{"handled": true, "post": map[string]any{"url": "https://www.instagram.example/p/Dfs6JqmNkyn/"}},
	}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "conteudo indisponivel [1]"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent(reelURL), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.calls) != 1 {
		t.Fatalf("an empty post must fall back to the web reader: %#v", service.calls)
	}
}
