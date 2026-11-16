// Package vision reads one image or video handed over as bytes through a local Ollama
// vision model and returns the visible text plus a short description. All input is
// untrusted: the bytes are data for the model, never instructions for the service. The
// caller fetches media; this service never makes requests of its own.
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrInvalidRequest = errors.New("invalid media request")
	ErrTooLarge       = errors.New("media exceeds the size limit")
	ErrFetchFailed    = errors.New("media is unreadable")
	ErrUpstream       = errors.New("vision model unavailable")
)

// DescribeRequest carries one media as base64 bytes: a caller that already holds the
// file (an attachment it fetched, a post reader's delivery) never needs this service to
// reach back out for it. ContentType is image/* or video/mp4.
type DescribeRequest struct {
	DataB64     string `json:"data_b64"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

// The reading prompt lives in the operator's vision_prompt.md (mounted into
// the container), so tuning a probabilistic behavior never needs a rebuild or
// a code change. There is deliberately no built-in prompt: Describe refuses
// to run without one.

type ollamaGenerate func(ctx context.Context, model, prompt string, images []string) (string, error)

// ErrNoPrompt means the service has no reading prompt configured. The prompt
// lives only in the operator's vision_prompt.md, never in code.
var ErrNoPrompt = errors.New("vision prompt is not configured")

// Service validates and describes one media at a time.
type Service struct {
	model     string
	prompt    string
	maxBytes  int64
	generate  ollamaGenerate
	maxOutput int
}

type Option func(*Service)

func WithModel(model string) Option {
	return func(s *Service) { s.model = model }
}
func WithPrompt(prompt string) Option {
	return func(s *Service) { s.prompt = prompt }
}
func WithMaxBytes(n int64) Option {
	return func(s *Service) { s.maxBytes = n }
}
func WithGenerate(generate ollamaGenerate) Option {
	return func(s *Service) { s.generate = generate }
}

func NewService(endpoint string, options ...Option) *Service {
	service := &Service{
		model:     "minicpm-v",
		maxBytes:  10 << 20,
		maxOutput: 4000,
	}
	for _, option := range options {
		option(service)
	}
	if service.generate == nil {
		service.generate = httpOllama{endpoint: strings.TrimRight(endpoint, "/"), timeout: 90 * time.Second}.Generate
	}
	return service
}

func (s *Service) Model() string { return s.model }

// Describe reads the media and returns the model's reading of it. An image is read as
// one picture; a video is sampled into frames that are read together, so the model sees
// scenes across the whole file rather than its first second.
func (s *Service) Describe(ctx context.Context, request DescribeRequest) (string, error) {
	if strings.TrimSpace(s.prompt) == "" {
		return "", ErrNoPrompt
	}
	data, video, err := validateRequest(request, s.maxBytes)
	if err != nil {
		return "", err
	}
	var frames []string
	if video {
		frames, err = extractFrames(ctx, data)
		if err != nil {
			return "", err
		}
	} else {
		frames = []string{base64.StdEncoding.EncodeToString(data)}
	}
	text, err := s.generate(ctx, s.model, s.prompt, frames)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(strings.ToValidUTF8(text, ""))
	if text == "" {
		return "", fmt.Errorf("%w: empty reading", ErrUpstream)
	}
	if runes := []rune(text); len(runes) > s.maxOutput {
		text = string(runes[:s.maxOutput]) + "…"
	}
	return text, nil
}

func validateRequest(request DescribeRequest, maxBytes int64) ([]byte, bool, error) {
	contentType := strings.ToLower(strings.TrimSpace(request.ContentType))
	video := contentType == "video/mp4"
	if !video && !strings.HasPrefix(contentType, "image/") {
		return nil, false, fmt.Errorf("%w: content_type must be image/* or video/mp4", ErrInvalidRequest)
	}
	data, err := base64.StdEncoding.Strict().DecodeString(request.DataB64)
	if err != nil || len(data) == 0 {
		return nil, false, fmt.Errorf("%w: data_b64 is not valid base64", ErrInvalidRequest)
	}
	if int64(len(data)) > maxBytes {
		return nil, false, ErrTooLarge
	}
	return data, video, nil
}

type httpOllama struct {
	endpoint string
	timeout  time.Duration
}

func (client httpOllama) Generate(ctx context.Context, model, prompt string, images []string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"prompt":     prompt,
		"images":     images,
		"stream":     false,
		"keep_alive": "10m",
		"options":    map[string]any{"num_predict": 512, "temperature": 0.2},
	})
	if err != nil {
		return "", err
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, client.endpoint+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if err != nil || len(payload) > 1<<20 {
		return "", fmt.Errorf("invalid ollama response")
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned status %d", response.StatusCode)
	}
	var decoded struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil || strings.TrimSpace(decoded.Response) == "" {
		return "", fmt.Errorf("invalid ollama response")
	}
	return decoded.Response, nil
}
