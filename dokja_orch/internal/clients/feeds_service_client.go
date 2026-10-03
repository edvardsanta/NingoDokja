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

const defaultFeedsServiceEndpoint = "tcp://127.0.0.1:5563"

type FeedsServiceRequesterFactory func() (repreq.RequesterReply, error)

// FeedsServiceClient talks to the feeds service over ZeroMQ. Requests are logged by type only.
type FeedsServiceClient struct {
	requesterFactory FeedsServiceRequesterFactory
}

type feedsServiceResponse struct {
	Status  string         `json:"status"`
	Result  map[string]any `json:"result"`
	Message string         `json:"message"`
}

func NewFeedsServiceClient(endpoint string) *FeedsServiceClient {
	resolved := resolveFeedsServiceEndpoint(endpoint)
	return &FeedsServiceClient{
		requesterFactory: func() (repreq.RequesterReply, error) {
			if err := validateFeedsServiceEndpoint(resolved); err != nil {
				return nil, err
			}
			return zeromq.NewRequester(resolved)
		},
	}
}

func NewFeedsServiceClientWithFactory(factory FeedsServiceRequesterFactory) *FeedsServiceClient {
	return &FeedsServiceClient{requesterFactory: factory}
}

// Dispatch sends one feeds.* event and returns the service's result.
func (c *FeedsServiceClient) Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if c == nil || c.requesterFactory == nil {
		return nil, fmt.Errorf("feeds service requester factory is not configured")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	logger.Info(fmt.Sprintf("feeds client sending request type=%s payload_keys=%d", eventType, len(payload)))

	body, err := json.Marshal(core.Event{Source: core.SourceOrchestrator, Type: eventType, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("marshal feeds event: %w", err)
	}

	requester, err := c.requesterFactory()
	if err != nil {
		return nil, fmt.Errorf("create feeds requester: %w", err)
	}
	defer requester.Close()

	responseCh := make(chan feedsServiceResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		raw, reqErr := requester.Request(string(body))
		if reqErr != nil {
			errCh <- reqErr
			return
		}
		var response feedsServiceResponse
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			errCh <- fmt.Errorf("unmarshal feeds response: %w", err)
			return
		}
		if response.Status != "ok" {
			if response.Message == "" {
				response.Message = "feeds service returned non-ok status"
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
		logger.Info(fmt.Sprintf("feeds client received response type=%s result_keys=%d", eventType, len(response.Result)))
		return response.Result, nil
	}
}

// Status reports the plugins the service follows and how each one is doing.
func (c *FeedsServiceClient) Status(ctx context.Context) (map[string]any, error) {
	return c.Dispatch(ctx, "feeds.status", nil)
}

func resolveFeedsServiceEndpoint(endpoint string) string {
	if endpoint != "" {
		return endpoint
	}
	if env := os.Getenv("FEEDS_SERVICE_ENDPOINT"); env != "" {
		return env
	}
	return defaultFeedsServiceEndpoint
}

func validateFeedsServiceEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("feeds service endpoint is empty")
	}
	if strings.HasPrefix(endpoint, "tcp://*:") || strings.HasPrefix(endpoint, "tcp://0.0.0.0:") {
		return fmt.Errorf(
			"feeds service endpoint %q is a bind address; configure a connect address such as tcp://dokja-feeds:5563",
			endpoint,
		)
	}
	return nil
}
