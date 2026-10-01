package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultEmbedEndpoint = "http://127.0.0.1:11434"
	DefaultEmbedModel    = "bge-m3"
)

// ErrEmbedUnavailable means the embedding server could not produce vectors: down, slow,
// model missing or a bad reply. The service keeps working without it.
var ErrEmbedUnavailable = errors.New("embedding server unavailable")

// Embedder turns text into unit-length vectors. It is optional: without one the service
// still stores experiences and only loses similarity.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Reachable(ctx context.Context) bool
}

// OllamaEmbedder talks to a local Ollama server. Each service in this project is
// self-contained, so this is a small copy of the knowledge and meme services' clients rather
// than a shared import. The timeout is short on purpose: experiences are recorded on the path
// of an action, and a slow embedder must not hold the service for long.
type OllamaEmbedder struct {
	endpoint string
	model    string
	client   *http.Client
}

func NewOllamaEmbedder(endpoint, model string, timeout time.Duration) *OllamaEmbedder {
	return &OllamaEmbedder{
		endpoint: strings.TrimRight(endpoint, "/"),
		model:    model,
		client:   &http.Client{Timeout: timeout},
	}
}

func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts, "truncate": true})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbedUnavailable, err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := e.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbedUnavailable, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 200))
		return nil, fmt.Errorf("%w: answered %d: %s", ErrEmbedUnavailable, response.StatusCode, strings.TrimSpace(string(detail)))
	}

	var payload struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: unreadable reply: %v", ErrEmbedUnavailable, err)
	}
	if len(payload.Embeddings) != len(texts) {
		return nil, fmt.Errorf("%w: returned %d vectors for %d texts", ErrEmbedUnavailable, len(payload.Embeddings), len(texts))
	}
	for i, vector := range payload.Embeddings {
		if len(vector) == 0 {
			return nil, fmt.Errorf("%w: returned an empty vector", ErrEmbedUnavailable)
		}
		payload.Embeddings[i] = normalize(vector)
	}
	return payload.Embeddings, nil
}

// Reachable asks the server for its model list, quickly, without embedding anything.
func (e *OllamaEmbedder) Reachable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, e.endpoint+"/api/tags", nil)
	if err != nil {
		return false
	}
	response, err := e.client.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func normalize(vector []float32) []float32 {
	var sum float64
	for _, value := range vector {
		sum += float64(value) * float64(value)
	}
	norm := math.Sqrt(sum)
	if norm == 0 {
		return vector
	}
	for i := range vector {
		vector[i] = float32(float64(vector[i]) / norm)
	}
	return vector
}
