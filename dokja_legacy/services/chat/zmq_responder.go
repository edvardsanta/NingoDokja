package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"read_books/internal/legacy/infrastructure/repreq"
	"read_books/internal/legacy/infrastructure/zeromq"
)

type RequesterFactory func() (repreq.RequesterReply, error)

type ZMQResponder struct {
	requesterFactory RequesterFactory
}

func NewZMQResponder(endpoint string) *ZMQResponder {
	return &ZMQResponder{
		requesterFactory: func() (repreq.RequesterReply, error) {
			return zeromq.NewRequester(endpoint)
		},
	}
}

func NewZMQResponderWithFactory(factory RequesterFactory) *ZMQResponder {
	return &ZMQResponder{requesterFactory: factory}
}

type requestEnvelope struct {
	EventType string         `json:"event_type"`
	Payload   requestPayload `json:"payload"`
}

type requestPayload struct {
	Content string `json:"content"`
	UserID  string `json:"userId"`
}

type responseEnvelope struct {
	Status string `json:"status"`
	Result string `json:"result"`
}

func (r *ZMQResponder) Respond(ctx context.Context, content, userID string) (string, error) {
	if r == nil || r.requesterFactory == nil {
		return "", fmt.Errorf("requester factory is not configured")
	}

	requester, err := r.requesterFactory()
	if err != nil {
		return "", fmt.Errorf("create requester: %w", err)
	}
	defer requester.Close()

	payload, err := json.Marshal(requestEnvelope{
		EventType: "chat_message",
		Payload: requestPayload{
			Content: content,
			UserID:  userID,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	responseCh := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		response, reqErr := requester.Request(string(payload))
		if reqErr != nil {
			errCh <- reqErr
			return
		}
		responseCh <- response
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case reqErr := <-errCh:
		return "", fmt.Errorf("request reply: %w", reqErr)
	case raw := <-responseCh:
		var parsed responseEnvelope
		if err = json.Unmarshal([]byte(raw), &parsed); err != nil {
			return "", fmt.Errorf("unmarshal response: %w", err)
		}
		if parsed.Status != "ok" {
			return "", fmt.Errorf("unexpected response status: %s", parsed.Status)
		}
		return parsed.Result, nil
	}
}
