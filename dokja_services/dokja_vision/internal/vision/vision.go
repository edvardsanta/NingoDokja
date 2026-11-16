// Package vision reads one image URL through a local Ollama vision model and
// returns the visible text plus a short description. All input is untrusted:
// the fetched bytes are data for the model, never instructions for the service.
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
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxURLBytes = 2048

var (
	ErrInvalidRequest = errors.New("invalid image request")
	ErrTooLarge       = errors.New("image exceeds the size limit")
	ErrFetchFailed    = errors.New("image download failed")
	ErrUpstream       = errors.New("vision model unavailable")
)

// DescribeRequest is platform metadata for one image, never bytes.
type DescribeRequest struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// DefaultPrompt asks for exactly what chat needs: the kind of image and its
// apparent context, the visible text preserved character by character
// (prices above all), and a short scene description, in Portuguese.
const DefaultPrompt = `Você lê imagens enviadas em um chat em português (Brasil).

Primeiro diga o TIPO da imagem e o contexto aparente em uma frase: captura de tela de página de loja/site, capa de jogo, captura de jogo, foto, print de conversa, gráfico, meme etc. Se for captura de tela, mencione o layout (página de qual site, janela de qual programa).

Depois transcreva o texto principal visível — título, preços, nomes, botões e rótulos — preservando números e valores caractere por caractere (R$ 874,21 continua R$ 874,21: nunca troque vírgula por ponto, não mova dígitos e não reformate). Se um texto aparecer repetido na imagem (listas de temas, tags), transcreva apenas a primeira ocorrência e nunca repita palavras em sequência.

Por fim, descreva em um parágrafo curto o que a imagem mostra e o que a pessoa provavelmente quer discutir nela.

Se não houver texto legível, descreva apenas a cena e o tipo. Responda somente com: tipo/contexto, transcrição e descrição.`

type ollamaGenerate func(ctx context.Context, model, prompt string, images []string) (string, error)

// Service validates, downloads and describes one image at a time.
type Service struct {
	model     string
	prompt    string
	maxBytes  int64
	fetch     *http.Client
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
func WithFetchClient(client *http.Client) Option {
	return func(s *Service) { s.fetch = client }
}
func WithGenerate(generate ollamaGenerate) Option {
	return func(s *Service) { s.generate = generate }
}

func NewService(endpoint string, options ...Option) *Service {
	service := &Service{
		model:     "minicpm-v",
		prompt:    DefaultPrompt,
		maxBytes:  10 << 20,
		maxOutput: 4000,
		fetch:     &http.Client{Timeout: 20 * time.Second},
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

// Describe downloads the image and returns the model's reading of it.
func (s *Service) Describe(ctx context.Context, request DescribeRequest) (string, error) {
	address, err := validateRequest(request, s.maxBytes)
	if err != nil {
		return "", err
	}
	image, contentType, err := s.download(ctx, address)
	if err != nil {
		return "", err
	}
	if contentType != "" && !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return "", fmt.Errorf("%w: download is not an image", ErrFetchFailed)
	}
	text, err := s.generate(ctx, s.model, s.prompt, []string{base64.StdEncoding.EncodeToString(image)})
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

func validateRequest(request DescribeRequest, maxBytes int64) (*url.URL, error) {
	if len(request.URL) > MaxURLBytes {
		return nil, fmt.Errorf("%w: url too long", ErrInvalidRequest)
	}
	address, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || address.Hostname() == "" || address.User != nil || (address.Scheme != "https" && address.Scheme != "http") {
		return nil, fmt.Errorf("%w: url must be http(s) without credentials", ErrInvalidRequest)
	}
	if request.Size > maxBytes {
		return nil, ErrTooLarge
	}
	return address, nil
}

func (s *Service) download(ctx context.Context, address *url.URL) ([]byte, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	response, err := s.fetch.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrFetchFailed, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%w: status %d", ErrFetchFailed, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, s.maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrFetchFailed, err)
	}
	if int64(len(body)) > s.maxBytes {
		return nil, "", ErrTooLarge
	}
	if !utf8.Valid(body) {
		// Image bytes are binary; validity is checked only to catch error pages
		// served as text with a 200 status.
		if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/") {
			return nil, "", fmt.Errorf("%w: text response", ErrFetchFailed)
		}
	}
	return body, response.Header.Get("Content-Type"), nil
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
