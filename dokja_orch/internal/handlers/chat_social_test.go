package handlers

import (
	"context"
	"errors"
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
	media      map[string]any
	socialErr  error
	invokeUrls []string
}

func (s *socialReadingService) Dispatch(ctx context.Context, kind string, payload map[string]any) (map[string]any, error) {
	switch kind {
	case "feeds.status":
		return map[string]any{"providers": s.readers}, nil
	case "feeds.invoke":
		action, _ := payload["action"].(string)
		if s.socialErr != nil && action == "read" {
			return nil, s.socialErr
		}
		input, _ := payload["input"].(map[string]any)
		s.invokeUrls = append(s.invokeUrls, fmt.Sprint(action, " ", input["url"]))
		if action == "media" {
			return s.media, nil
		}
		if action == "read" {
			return s.social, nil
		}
		return map[string]any{}, nil
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
	if len(service.invokeUrls) != 1 || service.invokeUrls[0] != "read "+reelURL {
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

type fakeSocialVision struct {
	text  string
	err   error
	calls []string
}

func (f *fakeSocialVision) DescribeMedia(_ context.Context, dataB64, contentType, filename string) (string, error) {
	f.calls = append(f.calls, contentType+"/"+filename)
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

func reelPostWithVideo() map[string]any {
	return map[string]any{"handled": true, "post": map[string]any{
		"url":       "https://www.instagram.example/p/Dfs6JqmNkyn/",
		"author":    "reelauthor",
		"caption":   "reels da madrugada",
		"published": "2026-11-14T00:22:55Z",
		"media":     []any{map[string]any{"kind": "video", "url": "http://127.0.0.1:8791/media/x.mp4"}},
	}}
}

func TestSocialPostMediaIsReadThroughVision(t *testing.T) {
	service := &socialReadingService{
		readers: socialReaderProvider(),
		social:  reelPostWithVideo(),
		media:   map[string]any{"data_b64": "RkFLRS1NUEQ=", "content_type": "video/mp4"},
	}
	vision := &fakeSocialVision{text: "um gato digitando no teclado"}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "achei o gato [1]"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil).WithSocialMediaVision(vision, nil)
	if _, err := h.Handle(context.Background(), chatEvent("olha "+reelURL), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.calls) != 0 {
		t.Fatalf("the web reader ran though the post reader answered: %#v", service.calls)
	}
	if len(vision.calls) != 1 || vision.calls[0] != "video/mp4/post-1.mp4" {
		t.Fatalf("vision did not receive the delivered media: %#v", vision.calls)
	}
	found := false
	for _, message := range generator.messages {
		if message["role"] == "user" && strings.Contains(message["content"], "um gato digitando") {
			found = true
		}
	}
	if !found {
		t.Fatal("the media reading never reached the generator")
	}
}

func TestSocialPostSurvivesVisionFailure(t *testing.T) {
	service := &socialReadingService{
		readers: socialReaderProvider(),
		social:  reelPostWithVideo(),
		media:   map[string]any{"data_b64": "RkFLRS1NUEQ=", "content_type": "video/mp4"},
	}
	vision := &fakeSocialVision{err: errors.New("vision down")}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "so a legenda [1]"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil).WithSocialMediaVision(vision, nil)
	result, err := h.Handle(context.Background(), chatEvent("olha "+reelURL), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result["reply"].(string)) == "" {
		t.Fatal("a vision failure must not drop the turn")
	}
	found := false
	for _, message := range generator.messages {
		if message["role"] == "user" && strings.Contains(message["content"], "reels da madrugada") && !strings.Contains(message["content"], "um gato digitando") {
			found = true
		}
	}
	if !found {
		t.Fatal("the caption-only reading must still reach the generator")
	}
}

func TestSocialPostMediaSkipsStreamedTokensAndDisabledVision(t *testing.T) {
	for name, setup := range map[string]struct {
		media   map[string]any
		enabled func() bool
	}{
		"streamedToken":  {media: map[string]any{"media_token": "tok", "content_type": "video/mp4", "size": 100}},
		"visionDisabled": {media: map[string]any{"data_b64": "RkFLRS1NUEQ=", "content_type": "video/mp4"}, enabled: func() bool { return false }},
	} {
		t.Run(name, func(t *testing.T) {
			service := &socialReadingService{
				readers: socialReaderProvider(),
				social:  reelPostWithVideo(),
				media:   setup.media,
			}
			vision := &fakeSocialVision{text: "um gato digitando"}
			generator := &fakeChatGenerator{result: map[string]any{"reply": "ok [1]"}}
			h := NewChatDomainHandler(generator).WithWebReading(service, nil).WithSocialMediaVision(vision, setup.enabled)
			if _, err := h.Handle(context.Background(), chatEvent("olha "+reelURL), chatStep); err != nil {
				t.Fatal(err)
			}
			if len(vision.calls) != 0 {
				t.Fatalf("vision must not be called: %#v", vision.calls)
			}
			for _, message := range generator.messages {
				if strings.Contains(message["content"], "um gato digitando") {
					t.Fatal("a media reading appeared without a described delivery")
				}
			}
		})
	}
}
