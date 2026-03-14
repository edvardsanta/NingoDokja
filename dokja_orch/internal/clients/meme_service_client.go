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

const defaultMemeServiceEndpoint = "tcp://127.0.0.1:5557"

type MemeServiceRequesterFactory func() (repreq.RequesterReply, error)

type MemeServiceClient struct {
	requesterFactory MemeServiceRequesterFactory
}

type MemeServiceResponse struct {
	Status  string         `json:"status"`
	Result  map[string]any `json:"result"`
	Message string         `json:"message"`
}

func NewMemeServiceClient(endpoint string) *MemeServiceClient {
	resolvedEndpoint := resolveMemeServiceEndpoint(endpoint)

	return &MemeServiceClient{
		requesterFactory: func() (repreq.RequesterReply, error) {
			if err := validateMemeServiceEndpoint(resolvedEndpoint); err != nil {
				return nil, err
			}
			return zeromq.NewRequester(resolvedEndpoint)
		},
	}
}

func NewMemeServiceClientWithFactory(factory MemeServiceRequesterFactory) *MemeServiceClient {
	return &MemeServiceClient{requesterFactory: factory}
}

func (c *MemeServiceClient) Fetch(ctx context.Context, limit *int) (map[string]any, error) {
	payload := map[string]any{}
	if limit != nil {
		payload["limit"] = *limit
	}

	response, err := c.dispatch(ctx, core.Event{
		Source:  core.SourceOrchestrator,
		Type:    "meme.fetch",
		Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (c *MemeServiceClient) RefreshPool(ctx context.Context, maxItemsPerScraper int) (map[string]any, error) {
	response, err := c.dispatch(ctx, core.Event{
		Source: core.SourceOrchestrator,
		Type:   "meme.pool.refresh",
		Payload: map[string]any{
			"max_items_per_scraper": maxItemsPerScraper,
		},
	})
	if err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (c *MemeServiceClient) Status(ctx context.Context) (map[string]any, error) {
	response, err := c.dispatch(ctx, core.Event{
		Source: core.SourceOrchestrator,
		Type:   "meme.status",
	})
	if err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (c *MemeServiceClient) dispatch(ctx context.Context, event core.Event) (MemeServiceResponse, error) {
	if c == nil || c.requesterFactory == nil {
		return MemeServiceResponse{}, fmt.Errorf("meme service requester factory is not configured")
	}
	logger.Info(fmt.Sprintf(
		"meme client sending request type=%s source=%s payload_keys=%d",
		event.Type,
		event.Source,
		len(event.Payload),
	))

	payload, err := json.Marshal(event)
	if err != nil {
		return MemeServiceResponse{}, fmt.Errorf("marshal meme event: %w", err)
	}

	requester, err := c.requesterFactory()
	if err != nil {
		return MemeServiceResponse{}, fmt.Errorf("create meme requester: %w", err)
	}
	defer requester.Close()

	responseCh := make(chan MemeServiceResponse, 1)
	errCh := make(chan error, 1)

	go func() {
		rawResponse, reqErr := requester.Request(string(payload))
		if reqErr != nil {
			errCh <- reqErr
			return
		}

		var response MemeServiceResponse
		if unmarshalErr := json.Unmarshal([]byte(rawResponse), &response); unmarshalErr != nil {
			errCh <- fmt.Errorf("unmarshal meme response: %w", unmarshalErr)
			return
		}
		logger.Info(fmt.Sprintf(
			"meme client received response type=%s status=%s result_keys=%d",
			event.Type,
			response.Status,
			len(response.Result),
		))

		if response.Status != "ok" {
			if response.Message == "" {
				response.Message = "meme service returned non-ok status"
			}
			errCh <- errors.New(response.Message)
			return
		}

		responseCh <- response
	}()

	select {
	case <-ctx.Done():
		return MemeServiceResponse{}, ctx.Err()
	case err := <-errCh:
		return MemeServiceResponse{}, err
	case response := <-responseCh:
		return response, nil
	}
}

func resolveMemeServiceEndpoint(endpoint string) string {
	if endpoint != "" {
		return endpoint
	}
	if envEndpoint := os.Getenv("MEME_SERVICE_ENDPOINT"); envEndpoint != "" {
		return envEndpoint
	}
	return defaultMemeServiceEndpoint
}

func validateMemeServiceEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("meme service endpoint is empty")
	}

	if strings.HasPrefix(endpoint, "tcp://*:") || strings.HasPrefix(endpoint, "tcp://0.0.0.0:") {
		return fmt.Errorf(
			"meme service endpoint %q is a bind address; configure a connect address such as tcp://dokja-meme:5557",
			endpoint,
		)
	}

	return nil
}
