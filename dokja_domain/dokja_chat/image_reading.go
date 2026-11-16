package chat

import (
	"net/url"
	"path"
	"strings"
)

const MaxImageReadings = 3

// MaxImageReadingBytes bounds one image handed to the vision service. The
// service enforces the same limit again on the bytes it downloads.
const MaxImageReadingBytes = 10 << 20

// ImageReadingRequest is one image a message carries that the vision service
// may read. Like WebReadingRequest, it is platform metadata, never bytes.
type ImageReadingRequest struct {
	URL, Filename, ContentType string
	Size                       int64
	Error                      string
}

// PlanImageReadings selects the images of a message for the vision service.
// Videos and other attachments are not image readings. URLs are deduplicated;
// the caller owns the per-message budget, as with web readings. Broken images
// stay in the plan (marked unreadable) so a repeated invalid URL cannot crowd
// out a valid one.
func PlanImageReadings(attachments []WebAttachment) []ImageReadingRequest {
	var requests []ImageReadingRequest
	seen := map[string]bool{}
	for _, attachment := range attachments {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(attachment.ContentType)), "image/") {
			continue
		}
		if seen[attachment.URL] {
			continue
		}
		request := ImageReadingRequest{
			URL:         attachment.URL,
			Filename:    path.Base(strings.ReplaceAll(attachment.Filename, "\\", "/")),
			ContentType: strings.ToLower(strings.TrimSpace(attachment.ContentType)),
			Size:        attachment.Size,
		}
		u, err := url.Parse(attachment.URL)
		if err != nil || len(attachment.URL) > 2048 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			request.Error = "unreadable"
		} else if attachment.Size < 0 || attachment.Size > MaxImageReadingBytes {
			request.Error = "too_large"
		}
		seen[attachment.URL] = true
		requests = append(requests, request)
	}
	return requests
}
