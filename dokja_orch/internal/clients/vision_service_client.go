package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	ErrVisionTooLarge    = errors.New("image exceeds the size limit")
	ErrVisionUnreadable  = errors.New("image could not be downloaded")
	ErrVisionUnavailable = errors.New("vision service unavailable")
)

// VisionServiceClient talks to the vision service over HTTP. The service has no
// authentication and lives only inside the compose network.
type VisionServiceClient struct {
	endpoint string
	http     *http.Client
}

func NewVisionServiceClient(endpoint string) *VisionServiceClient {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = os.Getenv("DOKJA_VISION_ENDPOINT")
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "http://dokja-vision:8082"
	}
	return &VisionServiceClient{strings.TrimRight(endpoint, "/"), &http.Client{Timeout: 100 * time.Second}}
}

// DescribeImage reads one image and returns the model's visible-text reading
// and short description.
func (c *VisionServiceClient) DescribeImage(ctx context.Context, url, filename, contentType string, size int64) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"url": url, "filename": filename, "content_type": contentType, "size": size,
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
	if err != nil || len(body) > 1<<20 {
		return "", fmt.Errorf("%w: invalid response", ErrVisionUnavailable)
	}
	if response.StatusCode != http.StatusOK {
		var decoded struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &decoded)
		switch response.StatusCode {
		case http.StatusRequestEntityTooLarge:
			return "", fmt.Errorf("%w: %s", ErrVisionTooLarge, decoded.Error)
		case http.StatusBadRequest, http.StatusBadGateway:
			return "", fmt.Errorf("%w: %s", ErrVisionUnreadable, decoded.Error)
		default:
			return "", fmt.Errorf("%w: status %d", ErrVisionUnavailable, response.StatusCode)
		}
	}
	var decoded struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil || strings.TrimSpace(decoded.Text) == "" {
		return "", fmt.Errorf("%w: invalid response", ErrVisionUnavailable)
	}
	return decoded.Text, nil
}
