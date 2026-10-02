package clients

import (
	"context"
	"fmt"
	"os"
	"read_books/internal/infrastructure/repreq"
	"read_books/internal/infrastructure/zeromq"
	"strings"
)

const defaultMemoryServiceEndpoint = "tcp://127.0.0.1:5562"

type MemoryServiceRequesterFactory func() (repreq.RequesterReply, error)

// MemoryServiceClient talks to the experience memory over ZeroMQ. Requests are logged by
// type only: their payload is the user's own text.
type MemoryServiceClient struct {
	requesterFactory MemoryServiceRequesterFactory
}

func NewMemoryServiceClient(endpoint string) *MemoryServiceClient {
	resolved := resolveMemoryServiceEndpoint(endpoint)
	return &MemoryServiceClient{
		requesterFactory: func() (repreq.RequesterReply, error) {
			if err := validateMemoryServiceEndpoint(resolved); err != nil {
				return nil, err
			}
			return zeromq.NewRequester(resolved)
		},
	}
}

func NewMemoryServiceClientWithFactory(factory MemoryServiceRequesterFactory) *MemoryServiceClient {
	return &MemoryServiceClient{requesterFactory: factory}
}

// Dispatch sends one memory.* event and returns the service's result.
func (c *MemoryServiceClient) Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if c == nil || c.requesterFactory == nil {
		return nil, fmt.Errorf("memory service requester factory is not configured")
	}
	return dispatchJSON(ctx, "memory", c.requesterFactory, eventType, payload)
}

// Status reports the experience counts and whether the embedder answers.
func (c *MemoryServiceClient) Status(ctx context.Context) (map[string]any, error) {
	return c.Dispatch(ctx, "memory.status", nil)
}

func resolveMemoryServiceEndpoint(endpoint string) string {
	if endpoint != "" {
		return endpoint
	}
	if env := os.Getenv("MEMORY_SERVICE_ENDPOINT"); env != "" {
		return env
	}
	return defaultMemoryServiceEndpoint
}

func validateMemoryServiceEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("memory service endpoint is empty")
	}
	if strings.HasPrefix(endpoint, "tcp://*:") || strings.HasPrefix(endpoint, "tcp://0.0.0.0:") {
		return fmt.Errorf(
			"memory service endpoint %q is a bind address; configure a connect address such as tcp://dokja-memory:5562",
			endpoint,
		)
	}
	return nil
}
