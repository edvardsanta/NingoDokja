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

const defaultChatAIServiceEndpoint = "http://127.0.0.1:8080"

type ChatAIServiceClient struct {
	endpoint   string
	httpClient *http.Client
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatAIServiceResponse struct {
	Status  string         `json:"status"`
	Result  map[string]any `json:"result"`
	Message string         `json:"message"`
}

func NewChatAIServiceClient(endpoint string) *ChatAIServiceClient {
	resolvedEndpoint := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if resolvedEndpoint == "" {
		resolvedEndpoint = strings.TrimRight(strings.TrimSpace(os.Getenv("CHAT_AI_SERVICE_ENDPOINT")), "/")
	}
	if resolvedEndpoint == "" {
		resolvedEndpoint = defaultChatAIServiceEndpoint
	}

	return &ChatAIServiceClient{
		endpoint: resolvedEndpoint,
		httpClient: &http.Client{
			Timeout: 2 * time.Minute,
		},
	}
}

func (c *ChatAIServiceClient) Generate(ctx context.Context, message string) (map[string]any, error) {
	return c.GenerateMessages(ctx, []map[string]string{{"role": "user", "content": message}})
}

func (c *ChatAIServiceClient) GenerateMessages(ctx context.Context, messages []map[string]string) (map[string]any, error) {
	if c == nil || c.httpClient == nil {
		return nil, fmt.Errorf("chat ai service client is not configured")
	}

	normalizedMessages := normalizeMessages(messages)
	body, err := json.Marshal(map[string]any{
		"message": firstUserMessage(normalizedMessages),
		"messages": normalizedMessages,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal chat ai request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build chat ai request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	logger.Info(fmt.Sprintf(
		"chat ai client sending request endpoint=%s message_chars=%d history_messages=%d",
		c.endpoint+"/chat",
		len(firstUserMessage(normalizedMessages)),
		len(normalizedMessages),
	))

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call chat ai service: %w", err)
	}
	defer response.Body.Close()

	var payload ChatAIServiceResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode chat ai response: %w", err)
	}
	logger.Info(fmt.Sprintf(
		"chat ai client received response endpoint=%s status_code=%d status=%s",
		c.endpoint+"/chat",
		response.StatusCode,
		payload.Status,
	))

	if response.StatusCode >= 400 {
		if payload.Message == "" {
			payload.Message = response.Status
		}
		return nil, fmt.Errorf("chat ai service error: %s", payload.Message)
	}
	if payload.Status != "ok" {
		if payload.Message == "" {
			payload.Message = "chat ai service returned non-ok status"
		}
		return nil, errors.New(payload.Message)
	}

	return payload.Result, nil
}

func normalizeMessages(messages []map[string]string) []ChatMessage {
	normalized := make([]ChatMessage, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message["role"])
		content := strings.TrimSpace(message["content"])
		if content == "" {
			continue
		}
		if role == "" {
			role = "user"
		}
		normalized = append(normalized, ChatMessage{
			Role:    role,
			Content: content,
		})
	}
	if len(normalized) == 0 {
		normalized = append(normalized, ChatMessage{Role: "user", Content: ""})
	}
	return normalized
}

func firstUserMessage(messages []ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].Content
}

func (c *ChatAIServiceClient) Health(ctx context.Context) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("chat ai service client is not configured")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/health", nil)
	if err != nil {
		return fmt.Errorf("build chat ai health request: %w", err)
	}

	logger.Info(fmt.Sprintf("chat ai client sending health request endpoint=%s", c.endpoint+"/health"))
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call chat ai health: %w", err)
	}
	defer response.Body.Close()

	var payload ChatAIServiceResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode chat ai health response: %w", err)
	}
	logger.Info(fmt.Sprintf(
		"chat ai client received health response endpoint=%s status_code=%d status=%s",
		c.endpoint+"/health",
		response.StatusCode,
		payload.Status,
	))

	if response.StatusCode >= 400 {
		if payload.Message == "" {
			payload.Message = response.Status
		}
		return fmt.Errorf("chat ai health error: %s", payload.Message)
	}
	if payload.Status != "ok" {
		if payload.Message == "" {
			payload.Message = "chat ai service returned non-ok health status"
		}
		return errors.New(payload.Message)
	}

	return nil
}
