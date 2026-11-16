package clients

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var (
	ErrVisionTooLarge    = errors.New("media exceeds the size limit")
	ErrVisionUnreadable  = errors.New("media could not be read")
	ErrVisionUnavailable = errors.New("vision service unavailable")
)

// maxVisionMediaBytes bounds one media handed to the vision service: inline post media
// is capped the same way, and the service decodes and checks again on its side.
const maxVisionMediaBytes = 16 << 20

// VisionServiceClient talks to the vision service over HTTP. The service has no
// authentication and lives only inside the compose network. It describes bytes the
// caller hands over — never fetches itself — so the client owns the one download an
// image URL needs.
type VisionServiceClient struct {
	endpoint string
	http     *http.Client
	fetch    *http.Client
}

func NewVisionServiceClient(endpoint string) *VisionServiceClient {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = os.Getenv("DOKJA_VISION_ENDPOINT")
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "http://dokja-vision:8082"
	}
	return &VisionServiceClient{
		endpoint: strings.TrimRight(endpoint, "/"),
		http:     &http.Client{Timeout: 100 * time.Second},
		fetch:    &http.Client{Timeout: 30 * time.Second},
	}
}

// DescribeImage downloads one image the caller holds an address for and returns the
// model's visible-text reading and short description.
func (c *VisionServiceClient) DescribeImage(ctx context.Context, address, filename, contentType string, size int64) (string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, kind, err := c.download(fetchCtx, address, size)
	if err != nil {
		return "", err
	}
	if contentType == "" {
		contentType = kind
	}
	return c.DescribeMedia(ctx, base64.StdEncoding.EncodeToString(data), contentType, filename)
}

// DescribeMedia describes media bytes the caller already holds: an attachment it
// fetched, or a post reader's delivery. Videos are sampled into frames by the service.
func (c *VisionServiceClient) DescribeMedia(ctx context.Context, dataB64, contentType, filename string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"data_b64": dataB64, "filename": filename, "content_type": contentType,
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/describe", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%w: %v", ErrVisionUnavailable, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil {
		return "", ErrVisionUnavailable
	}
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusRequestEntityTooLarge:
			return "", ErrVisionTooLarge
		case http.StatusBadGateway:
			return "", ErrVisionUnreadable
		}
		return "", ErrVisionUnavailable
	}
	var decoded struct {
		Text  string `json:"text"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil || strings.TrimSpace(decoded.Text) == "" {
		return "", ErrVisionUnavailable
	}
	return decoded.Text, nil
}

// download fetches one media address for DescribeImage: http(s) without credentials,
// an image or mp4 content type and the same byte cap the service enforces.
func (c *VisionServiceClient) download(ctx context.Context, raw string, size int64) ([]byte, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, "", fmt.Errorf("%w: invalid media address", ErrVisionUnreadable)
	}
	if size > maxVisionMediaBytes {
		return nil, "", ErrVisionTooLarge
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	response, err := c.fetch.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrVisionUnreadable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: status %d", ErrVisionUnreadable, response.StatusCode)
	}
	kind := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	if kind != "video/mp4" && !strings.HasPrefix(kind, "image/") {
		return nil, "", fmt.Errorf("%w: not an image or mp4", ErrVisionUnreadable)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxVisionMediaBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrVisionUnreadable, err)
	}
	if int64(len(data)) > maxVisionMediaBytes {
		return nil, "", ErrVisionTooLarge
	}
	return data, kind, nil
}
