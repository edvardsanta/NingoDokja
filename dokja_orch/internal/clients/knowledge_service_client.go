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
	"syscall"
	"time"

	"github.com/pebbe/zmq4"
)

const defaultKnowledgeServiceEndpoint = "tcp://127.0.0.1:5561"

// Leave ten seconds inside the desktop's two-minute ingest deadline to relay a
// timeout as an unknown write outcome. Fetching and embedding exceed the generic
// five-second request budget even when the service is healthy.
const knowledgeWriteTimeout = 110 * time.Second

type KnowledgeServiceRequesterFactory func() (repreq.RequesterReply, error)

// KnowledgeServiceClient talks to the research knowledge base over ZeroMQ. Requests are
// logged by type only: their payload is the user's private notes and questions.
type KnowledgeServiceClient struct {
	requesterFactory KnowledgeServiceRequesterFactory
	writeFactory     KnowledgeServiceRequesterFactory
}

type knowledgeServiceResponse struct {
	Status  string         `json:"status"`
	Result  map[string]any `json:"result"`
	Message string         `json:"message"`
}

func NewKnowledgeServiceClient(endpoint string) *KnowledgeServiceClient {
	resolved := resolveKnowledgeServiceEndpoint(endpoint)
	factory := func(timeout time.Duration) KnowledgeServiceRequesterFactory {
		return func() (repreq.RequesterReply, error) {
			if err := validateKnowledgeServiceEndpoint(resolved); err != nil {
				return nil, err
			}
			return zeromq.NewRequesterWithTimeout(resolved, timeout)
		}
	}
	return &KnowledgeServiceClient{requesterFactory: factory(5 * time.Second), writeFactory: factory(knowledgeWriteTimeout)}
}

func NewKnowledgeServiceClientWithFactory(factory KnowledgeServiceRequesterFactory) *KnowledgeServiceClient {
	return &KnowledgeServiceClient{requesterFactory: factory, writeFactory: factory}
}

// Dispatch sends one knowledge.* event and returns the service's result.
func (c *KnowledgeServiceClient) Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if c == nil || c.requesterFactory == nil {
		return nil, fmt.Errorf("knowledge service requester factory is not configured")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	logger.Info(fmt.Sprintf("knowledge client sending request type=%s payload_keys=%d", eventType, len(payload)))

	body, err := json.Marshal(core.Event{Source: core.SourceOrchestrator, Type: eventType, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("marshal knowledge event: %w", err)
	}

	factory := c.requesterFactory
	slowWrite := eventType == "knowledge.ingest" || eventType == "knowledge.reindex"
	if slowWrite {
		factory = c.writeFactory
	}
	if factory == nil {
		return nil, fmt.Errorf("knowledge service requester factory is not configured")
	}
	requester, err := factory()
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
		if slowWrite {
			return nil, unknownKnowledgeWrite(ctx.Err())
		}
		return nil, ctx.Err()
	case err := <-errCh:
		if slowWrite && zmq4.AsErrno(err) == zmq4.Errno(syscall.EAGAIN) {
			return nil, unknownKnowledgeWrite(err)
		}
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

// Status reports the knowledge base's counts and whether its embedder answers.
func (c *KnowledgeServiceClient) Status(ctx context.Context) (map[string]any, error) {
	return c.Dispatch(ctx, "knowledge.status", nil)
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

func unknownKnowledgeWrite(err error) error {
	return fmt.Errorf("knowledge_write_outcome_unknown: the reply deadline passed; the document may already be stored; reload the document list before retrying: %w", err)
}
