package digest

import (
	"context"
	"encoding/base64"
	"fmt"
)

const ActionReadLinkDelivery = "read-link-delivery"
const ActionMediaFile = "media_file"
const ActionMedia = "media"

// MaxDeliveryMediaBytes is WhatsApp's video limit. A file goes by token, in pieces, so size is
// not what holds it in memory.
const MaxDeliveryMediaBytes = 100_000_000
const MaxDeliveryTotalBytes = MaxDeliveryMediaBytes

// MaxInlineMediaBytes bounds a file from a reader that only has the "media" action: it travels
// whole, in base64, inside the reply.
const MaxInlineMediaBytes = 16 << 20
const MaxDeliveryMedia = 10

const refusedTooLarge = "media_too_large"

// DeliveryMedia carries either the bytes or a token the interface streams them with.
type DeliveryMedia struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	DataB64     string `json:"data_b64,omitempty"`
	MediaToken  string `json:"media_token,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

// MediaChunk is one piece of a file held by Feeds.
type MediaChunk struct {
	Data        []byte
	ContentType string
	Size        int64
}

// Delivery asks the same post reader for bounded media. Interfaces never fetch reader URLs, which
// may refer to a private host-local media cache: they stream a host token through the orchestrator.
func (d *Domain) readLinkDelivery(ctx context.Context, payload map[string]any) (map[string]any, error) {
	result, err := d.readLink(ctx, payload)
	if err != nil || result["status"] != "ok" {
		return result, err
	}
	post, _ := result["post"].(map[string]any)
	reader, _ := post["reader"].(string)
	rows, _ := post["media"].([]map[string]any)
	if len(rows) == 0 {
		return refused(RefusedUnavailable), nil
	}
	if len(rows) > MaxDeliveryMedia {
		return refused(refusedTooLarge), nil
	}
	media := []DeliveryMedia{}
	var total int64
	for index, row := range rows {
		item, size, refusal := d.deliveryFile(ctx, reader, row)
		if refusal != "" {
			return refused(refusal), nil
		}
		total += size
		if total > MaxDeliveryTotalBytes {
			return refused(refusedTooLarge), nil
		}
		item.Filename = fmt.Sprintf("post-%d.%s", index+1, deliveryExtension(item.ContentType))
		media = append(media, item)
	}
	return map[string]any{"status": "ok", "post": post, "media": media}, nil
}

func (d *Domain) deliveryFile(ctx context.Context, reader string, row map[string]any) (DeliveryMedia, int64, string) {
	ask := func(action string) (map[string]any, error) {
		return d.service.Dispatch(ctx, "feeds.invoke", map[string]any{"capability": SocialCapability, "action": action, "plugin": reader, "input": map[string]any{"url": row["url"]}})
	}
	reply, err := ask(ActionMediaFile)
	if err == nil {
		if reason, _ := reply["refused"].(string); reason == refusedTooLarge {
			return DeliveryMedia{}, 0, reason
		}
		if token, _ := reply["media_token"].(string); token != "" {
			return tokenFile(reply, token, row)
		}
	}
	if reply, err = ask(ActionMedia); err != nil {
		return DeliveryMedia{}, 0, RefusedUnavailable
	}
	return inlineFile(reply, row)
}

func tokenFile(reply map[string]any, token string, row map[string]any) (DeliveryMedia, int64, string) {
	mime, _ := reply["content_type"].(string)
	size := wholeBytes(reply["size"])
	if !fitsKind(mime, row) || size <= 0 {
		return DeliveryMedia{}, 0, RefusedFailed
	}
	if size > MaxDeliveryMediaBytes {
		return DeliveryMedia{}, 0, refusedTooLarge
	}
	return DeliveryMedia{ContentType: mime, MediaToken: token, Size: size}, size, ""
}

func inlineFile(reply map[string]any, row map[string]any) (DeliveryMedia, int64, string) {
	if reason, _ := reply["refused"].(string); reason == refusedTooLarge {
		return DeliveryMedia{}, 0, reason
	}
	encoded, _ := reply["data_b64"].(string)
	mime, _ := reply["content_type"].(string)
	if !fitsKind(mime, row) {
		return DeliveryMedia{}, 0, RefusedFailed
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxInlineMediaBytes) {
		return DeliveryMedia{}, 0, refusedTooLarge
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return DeliveryMedia{}, 0, RefusedFailed
	}
	if len(data) > MaxInlineMediaBytes {
		return DeliveryMedia{}, 0, refusedTooLarge
	}
	return DeliveryMedia{ContentType: mime, DataB64: encoded}, int64(len(data)), ""
}

func fitsKind(mime string, row map[string]any) bool {
	return deliveryExtension(mime) != "" && (row["kind"] == "video") == (mime == "video/mp4")
}

func deliveryExtension(mime string) string {
	switch mime {
	case "image/jpeg":
		return "jpg"
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	case "video/mp4":
		return "mp4"
	}
	return ""
}

// wholeBytes reads a size that crossed JSON as a number.
func wholeBytes(value any) int64 {
	switch number := value.(type) {
	case float64:
		if number == float64(int64(number)) {
			return int64(number)
		}
	case int64:
		return number
	}
	return 0
}

// MediaChunk reads one piece of a file a delivery named. The token came from Feeds, and Feeds is
// what judges it.
func (d *Domain) MediaChunk(ctx context.Context, token string, offset int64) (MediaChunk, error) {
	reply, err := d.service.Dispatch(ctx, "feeds.media.read", map[string]any{"token": token, "offset": offset})
	if err != nil {
		return MediaChunk{}, err
	}
	encoded, _ := reply["data_b64"].(string)
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return MediaChunk{}, fmt.Errorf("media piece is not valid base64")
	}
	mime, _ := reply["content_type"].(string)
	size := wholeBytes(reply["size"])
	if len(data) == 0 || deliveryExtension(mime) == "" || size <= 0 || size > MaxDeliveryMediaBytes {
		return MediaChunk{}, fmt.Errorf("media piece is invalid")
	}
	return MediaChunk{Data: data, ContentType: mime, Size: size}, nil
}
