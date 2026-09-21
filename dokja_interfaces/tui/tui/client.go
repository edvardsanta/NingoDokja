package tui

import (
	"context"
	"fmt"

	"dokja_interfaces/cli/cli"
)

// Client sends one event to the orchestrator and returns the domain's result map.
type Client interface {
	Request(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

// OrchestratorClient opens a fresh REQ socket per request: ZeroMQ REQ is strict
// send/recv lockstep, so sockets are never shared or reused after a timeout.
type OrchestratorClient struct {
	Endpoint string
}

func NewOrchestratorClient(endpoint string) *OrchestratorClient {
	return &OrchestratorClient{Endpoint: endpoint}
}

func (c *OrchestratorClient) Request(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	requester, err := cli.NewRequester(c.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect to orchestrator: %w", err)
	}
	defer requester.Close()

	event := cli.BuildEvent(cli.EmitOptions{
		Type:      eventType,
		UserID:    "tui-user",
		UserName:  "tui",
		ChannelID: "tui",
		Payload:   payload,
		Context:   map[string]any{"interface": "tui"},
		Source:    "cli",
	})
	response, err := requester.Request(ctx, event)
	if err != nil {
		return nil, err
	}
	return unwrapResult(response.Result)
}

// unwrapResult turns {"event_id":..,"workflow":..,"result":{"<domain>":{...}}} into {...}.
func unwrapResult(raw any) (map[string]any, error) {
	envelope, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected orchestrator response: %T", raw)
	}
	inner, ok := envelope["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("orchestrator response has no result")
	}
	if len(inner) == 1 {
		for _, value := range inner {
			if domain, ok := value.(map[string]any); ok {
				return domain, nil
			}
		}
	}
	return inner, nil
}
