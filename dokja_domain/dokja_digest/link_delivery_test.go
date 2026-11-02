package digest

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

type deliveryReader struct {
	data, mime string
	fail       bool
	refused    string
	asked      []string
	media      []any
	// file is what media_file answers; without it the reader only has the older action.
	file map[string]any
}

func (r *deliveryReader) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	if event == "feeds.status" {
		return map[string]any{"providers": []any{provider("reader", true, SocialCapability)}}, nil
	}
	if event == "feeds.media.read" {
		return r.file, nil
	}
	action, _ := payload["action"].(string)
	r.asked = append(r.asked, action)
	if payload["plugin"] != "reader" {
		return nil, errors.New("wrong reader")
	}
	if action == "read" {
		return map[string]any{"handled": true, "post": map[string]any{"url": "https://example.com/post", "caption": "Original caption", "media": r.media}}, nil
	}
	if action == "media_file" && r.file == nil {
		return map[string]any{"handled": false}, nil
	}
	if action == "media_file" {
		return r.file, nil
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
	if len(media) != 1 || media[0].ContentType != "video/mp4" || media[0].DataB64 != reader.data || len(reader.asked) != 3 || reader.asked[1] != "media_file" || reader.asked[2] != "media" {
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
	data := base64.StdEncoding.EncodeToString(make([]byte, MaxInlineMediaBytes))
	reader := &deliveryReader{data: data, mime: "video/mp4", media: []any{map[string]any{"kind": "video", "url": "http://localhost/media/opaque"}}}
	reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
	if err != nil || reply["status"] != "ok" {
		t.Fatalf("a video at the limit was refused: %v %+v", err, reply["refused"])
	}
}

func TestPostDeliveryReportsOversizedMediaAsTooLarge(t *testing.T) {
	oversized := base64.StdEncoding.EncodeToString(make([]byte, MaxInlineMediaBytes+1))
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

func TestPostDeliveryNamesALargeFileByItsTokenWithoutItsBytes(t *testing.T) {
	token := strings.Repeat("a", 48)
	reader := &deliveryReader{file: map[string]any{"media_token": token, "content_type": "video/mp4", "size": float64(92_923_063)},
		media: []any{map[string]any{"kind": "video", "url": "http://localhost/media/opaque"}}}
	reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
	if err != nil || reply["status"] != "ok" {
		t.Fatalf("delivery failed: %v %+v", err, reply)
	}
	media := reply["media"].([]DeliveryMedia)
	if len(media) != 1 || media[0].MediaToken != token || media[0].Size != 92_923_063 || media[0].DataB64 != "" || media[0].Filename != "post-1.mp4" {
		t.Fatalf("token contract lost: %+v", media)
	}
	if len(reader.asked) != 2 {
		t.Fatalf("a reader with media_file was asked again: %v", reader.asked)
	}
}

func TestPostDeliveryJudgesATokenFile(t *testing.T) {
	video := []any{map[string]any{"kind": "video", "url": "http://localhost/media/opaque"}}
	for name, tc := range map[string]struct {
		file    map[string]any
		refusal string
	}{
		"over the limit":  {map[string]any{"media_token": "t", "content_type": "video/mp4", "size": float64(MaxDeliveryMediaBytes + 1)}, "media_too_large"},
		"refused":         {map[string]any{"refused": "media_too_large"}, "media_too_large"},
		"no size":         {map[string]any{"media_token": "t", "content_type": "video/mp4"}, RefusedFailed},
		"wrong kind":      {map[string]any{"media_token": "t", "content_type": "image/png", "size": float64(1)}, RefusedFailed},
		"unexpected type": {map[string]any{"media_token": "t", "content_type": "text/html", "size": float64(1)}, RefusedFailed},
	} {
		t.Run(name, func(t *testing.T) {
			reply, err := New(&deliveryReader{file: tc.file, media: video}).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
			if err != nil || reply["refused"] != tc.refusal {
				t.Fatalf("wrong refusal: %v %+v", err, reply)
			}
		})
	}
}

func TestPostDeliveryBoundsTheTotalAcrossTokenFiles(t *testing.T) {
	reader := &deliveryReader{file: map[string]any{"media_token": "t", "content_type": "video/mp4", "size": float64(MaxDeliveryMediaBytes)},
		media: []any{map[string]any{"kind": "video", "url": "http://localhost/media/1"}, map[string]any{"kind": "video", "url": "http://localhost/media/2"}}}
	reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
	if err != nil || reply["refused"] != "media_too_large" {
		t.Fatalf("wrong refusal: %v %+v", err, reply)
	}
}

func TestMediaChunkPassesOnOnePiece(t *testing.T) {
	reader := &deliveryReader{file: map[string]any{"data_b64": "Y2xpcA==", "content_type": "video/mp4", "size": float64(4)}}
	piece, err := New(reader).MediaChunk(context.Background(), "token", 0)
	if err != nil || string(piece.Data) != "clip" || piece.Size != 4 || piece.ContentType != "video/mp4" {
		t.Fatal(piece, err)
	}
	for name, file := range map[string]map[string]any{
		"bad base64": {"data_b64": "bad!", "content_type": "video/mp4", "size": float64(4)},
		"empty":      {"data_b64": "", "content_type": "video/mp4", "size": float64(4)},
		"too large":  {"data_b64": "Y2xpcA==", "content_type": "video/mp4", "size": float64(MaxDeliveryMediaBytes + 1)},
		"bad type":   {"data_b64": "Y2xpcA==", "content_type": "text/html", "size": float64(4)},
	} {
		if _, err := New(&deliveryReader{file: file}).MediaChunk(context.Background(), "token", 0); err == nil {
			t.Fatal(name)
		}
	}
}

func TestPostDeliveryKeepsTheCombinedInlineLimit(t *testing.T) {
	reader := &deliveryReader{data: base64.StdEncoding.EncodeToString(make([]byte, MaxInlineMediaBytes/2+1)), mime: "video/mp4",
		media: []any{map[string]any{"kind": "video", "url": "https://media.example/1"}, map[string]any{"kind": "video", "url": "https://media.example/2"}}}
	reply, err := New(reader).Handle(context.Background(), Request{Action: ActionReadLinkDelivery, Event: Event{Payload: map[string]any{"url": "https://example.com/post"}}})
	if err != nil || reply["refused"] != "media_too_large" {
		t.Fatalf("inline files exceeded the adapter's combined limit: %v %+v", err, reply)
	}
}
