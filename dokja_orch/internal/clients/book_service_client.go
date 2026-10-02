package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"read_books/internal/logger"
	"strings"
	"time"
)

const defaultBookServiceEndpoint = "http://127.0.0.1:8083"

// bookHealthTimeout bounds a health probe; the client's own timeout is sized for summarizing.
const bookHealthTimeout = 5 * time.Second

type BookServiceClient struct {
	endpoint   string
	httpClient *http.Client
}

type BookServiceResponse struct {
	Status  string         `json:"status"`
	Result  map[string]any `json:"result"`
	Message string         `json:"message"`
}

func NewBookServiceClient(endpoint string) *BookServiceClient {
	resolvedEndpoint := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if resolvedEndpoint == "" {
		resolvedEndpoint = strings.TrimRight(strings.TrimSpace(os.Getenv("BOOK_SERVICE_ENDPOINT")), "/")
	}
	if resolvedEndpoint == "" {
		resolvedEndpoint = defaultBookServiceEndpoint
	}

	return &BookServiceClient{
		endpoint: resolvedEndpoint,
		httpClient: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (c *BookServiceClient) Summarize(ctx context.Context, input map[string]any) (map[string]any, error) {
	return c.post(ctx, "/books/summarize", input)
}

func (c *BookServiceClient) Classify(ctx context.Context, input map[string]any) (map[string]any, error) {
	return c.post(ctx, "/books/classify", input)
}

// Health asks the book service whether it is up (GET /health).
func (c *BookServiceClient) Health(ctx context.Context) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("book service client is not configured")
	}

	ctx, cancel := context.WithTimeout(ctx, bookHealthTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return fmt.Errorf("build book health request: %w", err)
	}

	logger.Info(fmt.Sprintf("book service client sending health request endpoint=%s", c.endpoint+"/health"))
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call book health: %w", err)
	}
	defer response.Body.Close()

	var payload BookServiceResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&payload)
	if response.StatusCode >= 400 {
		if payload.Message == "" {
			payload.Message = response.Status
		}
		return fmt.Errorf("book health error: %s", payload.Message)
	}
	if decodeErr != nil {
		return fmt.Errorf("decode book health response: %w", decodeErr)
	}
	if payload.Status != "ok" {
		if payload.Message == "" {
			payload.Message = "book service returned non-ok health status"
		}
		return errors.New(payload.Message)
	}
	return nil
}

func (c *BookServiceClient) post(ctx context.Context, path string, input map[string]any) (map[string]any, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("book service client is not configured")
	}

	body, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal book service request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build book service request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	logger.Info(fmt.Sprintf("book service client sending request endpoint=%s payload_keys=%d", c.endpoint+path, len(input)))

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call book service: %w", err)
	}
	defer response.Body.Close()

	var payload BookServiceResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode book service response: %w", err)
	}

	if response.StatusCode >= 400 {
		if payload.Message == "" {
			payload.Message = response.Status
		}
		return nil, fmt.Errorf("book service error: %s", payload.Message)
	}
	if payload.Status != "ok" {
		if payload.Message == "" {
			payload.Message = "book service returned non-ok status"
		}
		return nil, errors.New(payload.Message)
	}

	return payload.Result, nil
}
