package digest

import (
	"context"
	"encoding/base64"
	"fmt"
)

const ActionReadLinkDelivery = "read-link-delivery"
const MaxDeliveryMediaBytes = 16 << 20
const MaxDeliveryTotalBytes = 16 << 20
const MaxDeliveryMedia = 10

type DeliveryMedia struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	DataB64     string `json:"data_b64"`
}

// Delivery asks the same post reader for bounded media bytes. Interfaces never
// fetch reader URLs, which may refer to a private host-local media cache.
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
		return refused("media_too_large"), nil
	}
	media := []DeliveryMedia{}
	total := 0
	for index, row := range rows {
		reply, err := d.service.Dispatch(ctx, "feeds.invoke", map[string]any{"capability": SocialCapability, "action": "media", "plugin": reader, "input": map[string]any{"url": row["url"]}})
		if err != nil {
			return refused(RefusedUnavailable), nil
		}
		if reason, _ := reply["refused"].(string); reason == "media_too_large" {
			return refused(reason), nil
		}
		encoded, _ := reply["data_b64"].(string)
		mime, _ := reply["content_type"].(string)
		extension := ""
		switch mime {
		case "image/jpeg":
			extension = "jpg"
		case "image/png":
			extension = "png"
		case "image/webp":
			extension = "webp"
		case "video/mp4":
			extension = "mp4"
		}
		if extension == "" || (row["kind"] == "video") != (mime == "video/mp4") {
			return refused(RefusedFailed), nil
		}
		if len(encoded) > base64.StdEncoding.EncodedLen(MaxDeliveryMediaBytes) {
			return refused("media_too_large"), nil
		}
		data, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(data) == 0 {
			return refused(RefusedFailed), nil
		}
		total += len(data)
		if len(data) > MaxDeliveryMediaBytes || total > MaxDeliveryTotalBytes {
			return refused("media_too_large"), nil
		}
		media = append(media, DeliveryMedia{Filename: fmt.Sprintf("post-%d.%s", index+1, extension), ContentType: mime, DataB64: encoded})
	}
	return map[string]any{"status": "ok", "post": post, "media": media}, nil
}
