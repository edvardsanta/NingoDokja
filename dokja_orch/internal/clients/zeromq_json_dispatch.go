package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"read_books/internal/core"
	"read_books/internal/infrastructure/repreq"
	"read_books/internal/logger"
)

// serviceResponse is the envelope the Python and Go services answer with over ZeroMQ.
type serviceResponse struct {
	Status  string         `json:"status"`
	Result  map[string]any `json:"result"`
	Message string         `json:"message"`
}

// dispatchJSON sends one event over a fresh requester and returns the service's result. It
// logs the event type and the size of the payload only: a payload is the user's own text.
// name is what the service is called in logs and errors.
func dispatchJSON(
	ctx context.Context,
	name string,
	factory func() (repreq.RequesterReply, error),
	eventType string,
	payload map[string]any,
) (map[string]any, error) {
	if factory == nil {
		return nil, fmt.Errorf("%s service requester factory is not configured", name)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	logger.Info(fmt.Sprintf("%s client sending request type=%s payload_keys=%d", name, eventType, len(payload)))

	body, err := json.Marshal(core.Event{Source: core.SourceOrchestrator, Type: eventType, Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("marshal %s event: %w", name, err)
	}

	requester, err := factory()
	if err != nil {
		return nil, fmt.Errorf("create %s requester: %w", name, err)
	}
	defer requester.Close()

	responseCh := make(chan serviceResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		raw, reqErr := requester.Request(string(body))
		if reqErr != nil {
			errCh <- reqErr
			return
		}
		var response serviceResponse
		if err := json.Unmarshal([]byte(raw), &response); err != nil {
			errCh <- fmt.Errorf("unmarshal %s response: %w", name, err)
			return
		}
		if response.Status != "ok" {
			if response.Message == "" {
				response.Message = name + " service returned non-ok status"
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
		logger.Info(fmt.Sprintf("%s client received response type=%s result_keys=%d", name, eventType, len(response.Result)))
		return response.Result, nil
	}
}
