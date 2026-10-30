package digest

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

type deliveryReader struct {
	data, mime string
	fail       bool
	refused    string
	asked      []string
	media      []any
}

func (r *deliveryReader) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	if event == "feeds.status" {
		return map[string]any{"providers": []any{provider("reader", true, SocialCapability)}}, nil
	}
	action, _ := payload["action"].(string)
	r.asked = append(r.asked, action)
	if payload["plugin"] != "reader" {
		return nil, errors.New("wrong reader")
	}
	if action == "read" {
		return map[string]any{"handled": true, "post": map[string]any{"url": "https://example.com/post", "caption": "Original caption", "media": r.media}}, nil
	}
	if r.fail {
		return nil, errors.New("media unavailable")
	}
	if r.refused != "" {
		return map[string]any{"refused": r.refused}, nil
	}
	return map[string]any{"data_b64": r.data, "content_type": r.mime}, nil
}
func TestPostDeliveryGetsMediaFromTheSelectedReader(t *testing.T) {
	reader := &deliveryReader{data: base64.StdEncoding.EncodeToString([]byte("video")), mime: "video/mp4", media: []any{map[string]any{"kind": "video", "url": "http://localhost/media/opaque"}}}
	reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
	if err != nil || reply["status"] != "ok" {
		t.Fatalf("delivery failed: %v %+v", err, reply)
	}
	media := reply["media"].([]DeliveryMedia)
	if len(media) != 1 || media[0].ContentType != "video/mp4" || media[0].DataB64 != reader.data || len(reader.asked) != 2 || reader.asked[1] != "media" {
		t.Fatal("media contract lost")
	}
}
func TestPostDeliveryRefusesInvalidOrUnavailableMedia(t *testing.T) {
	for _, tc := range []struct {
		name, data, mime string
		fail             bool
	}{{"invalid base64", "bad!", "video/mp4", false}, {"wrong kind", "eA==", "image/png", false}, {"unsafe type", "eA==", "application/javascript", false}, {"missing media", "", "video/mp4", false}, {"unavailable", "eA==", "video/mp4", true}} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &deliveryReader{data: tc.data, mime: tc.mime, fail: tc.fail, media: []any{map[string]any{"kind": "video", "url": "https://media.example/post"}}}
			reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
			if err != nil || reply["status"] != "refused" || reply["media"] != nil {
				t.Fatal("invalid media delivered")
			}
		})
	}
}

func TestPostDeliveryAcceptsAVideoUpToTheFileLimit(t *testing.T) {
	data := base64.StdEncoding.EncodeToString(make([]byte, MaxDeliveryMediaBytes))
	reader := &deliveryReader{data: data, mime: "video/mp4", media: []any{map[string]any{"kind": "video", "url": "http://localhost/media/opaque"}}}
	reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
	if err != nil || reply["status"] != "ok" {
		t.Fatalf("a video at the limit was refused: %v %+v", err, reply["refused"])
	}
}

func TestPostDeliveryReportsOversizedMediaAsTooLarge(t *testing.T) {
	oversized := base64.StdEncoding.EncodeToString(make([]byte, MaxDeliveryMediaBytes+1))
	for name, reader := range map[string]*deliveryReader{
		"refused by the reader":  {refused: "media_too_large"},
		"returned by the reader": {data: oversized, mime: "video/mp4"},
	} {
		t.Run(name, func(t *testing.T) {
			reader.media = []any{map[string]any{"kind": "video", "url": "http://localhost/media/opaque"}}
			reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
			if err != nil || reply["status"] != "refused" || reply["refused"] != "media_too_large" {
				t.Fatalf("wrong refusal: %v %+v", err, reply)
			}
		})
	}
}
