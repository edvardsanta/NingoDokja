package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"read_books/internal/core"
	"read_books/internal/infrastructure/repreq"
	"read_books/internal/infrastructure/zeromq"
	"read_books/internal/logger"
	"strings"
)

const defaultKnowledgeServiceEndpoint = "tcp://127.0.0.1:5561"

type KnowledgeServiceRequesterFactory func() (repreq.RequesterReply, error)

// KnowledgeServiceClient talks to the research knowledge base over ZeroMQ. Requests are
// logged by type only: their payload is the user's private notes and questions.
type KnowledgeServiceClient struct {
	requesterFactory KnowledgeServiceRequesterFactory
}

type knowledgeServiceResponse struct {
	Status  string         `json:"status"`
	Result  map[string]any `json:"result"`
	Message string         `json:"message"`
}

func NewKnowledgeServiceClient(endpoint string) *KnowledgeServiceClient {
	resolved := resolveKnowledgeServiceEndpoint(endpoint)
	return &KnowledgeServiceClient{
		requesterFactory: func() (repreq.RequesterReply, error) {
			if err := validateKnowledgeServiceEndpoint(resolved); err != nil {
				return nil, err
			}
			return zeromq.NewRequester(resolved)
		},
	}
}

func NewKnowledgeServiceClientWithFactory(factory KnowledgeServiceRequesterFactory) *KnowledgeServiceClient {
	return &KnowledgeServiceClient{requesterFactory: factory}
}

// Dispatch sends one knowledge.* event and returns the service's result.
func (c *KnowledgeServiceClient) Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if c == nil || c.requesterFactory == nil {
		return nil, fmt.Errorf("knowledge service requester factory is not configured")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	logger.Info(fmt.Sprintf("knowledge client sending request type=%s payload_keys=%d", eventType, len(payload)))

	body, err := json.Marshal(core.Event{Source: core.SourceOrchestrator, Type: eventType, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("marshal knowledge event: %w", err)
	}

	requester, err := c.requesterFactory()
	if err != nil {
		return nil, fmt.Errorf("create knowledge requester: %w", err)
	}
	defer requester.Close()

	responseCh := make(chan knowledgeServiceResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		raw, reqErr := requester.Request(string(body))
		if reqErr != nil {
			errCh <- reqErr
			return
		}
		var response knowledgeServiceResponse
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			errCh <- fmt.Errorf("unmarshal knowledge response: %w", err)
			return
		}
		if response.Status != "ok" {
			if response.Message == "" {
				response.Message = "knowledge service returned non-ok status"
			}
			errCh <- errors.New(response.Message)
			return
		}
		responseCh <- response
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-errCh:
		return nil, err
	case response := <-responseCh:
		logger.Info(fmt.Sprintf("knowledge client received response type=%s result_keys=%d", eventType, len(response.Result)))
		return response.Result, nil
	}
}

// Search asks for the k closest chunks to query.
func (c *KnowledgeServiceClient) Search(ctx context.Context, query string, k int) (map[string]any, error) {
	return c.Dispatch(ctx, "knowledge.search", map[string]any{"query": query, "k": k})
}

func resolveKnowledgeServiceEndpoint(endpoint string) string {
	if endpoint != "" {
		return endpoint
	}
	if env := os.Getenv("KNOWLEDGE_SERVICE_ENDPOINT"); env != "" {
		return env
	}
	return defaultKnowledgeServiceEndpoint
}

func validateKnowledgeServiceEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("knowledge service endpoint is empty")
	}
	if strings.HasPrefix(endpoint, "tcp://*:") || strings.HasPrefix(endpoint, "tcp://0.0.0.0:") {
		return fmt.Errorf(
			"knowledge service endpoint %q is a bind address; configure a connect address such as tcp://dokja-knowledge:5561",
			endpoint,
		)
	}
	return nil
}
