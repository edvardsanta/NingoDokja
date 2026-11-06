package tui

import (
	"context"
	"errors"
	"fmt"
	"os"

	"dokja_interfaces/cli/cli"
)

// Client sends one event to the orchestrator and returns the domain's result map.
type Client interface {
	Request(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

// ChatStreamer is a client that can send an archived chat turn and say where it stands while it
// waits. streamed is false when it could not: then nothing was sent, and the turn goes by Request.
type ChatStreamer interface {
	StreamChat(ctx context.Context, payload map[string]any, progress func(state string)) (result map[string]any, streamed bool, err error)
}

// OrchestratorClient opens a fresh REQ socket per request: ZeroMQ REQ is strict
// send/recv lockstep, so sockets are never shared or reused after a timeout.
type OrchestratorClient struct {
	Endpoint string
	// Bridge is the orchestrator's chat stream on this machine, when it is configured.
	Bridge *cli.BridgeStream
}

func NewOrchestratorClient(endpoint string) *OrchestratorClient {
	bridge, _ := cli.BridgeFromEnv()
	return &OrchestratorClient{Endpoint: endpoint, Bridge: bridge}
}

func tuiEvent(eventType string, payload map[string]any) cli.Event {
	return cli.BuildEvent(cli.EmitOptions{
		Type:      eventType,
		UserID:    "tui-user",
		UserName:  "tui",
		ChannelID: "tui",
		Payload:   payload,
		Context:   cli.CommunicationsContext(eventType, map[string]any{"interface": "tui"}, os.Getenv),
		Source:    "cli",
	})
}

func (c *OrchestratorClient) Request(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	requester, err := cli.NewRequester(c.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("connect to orchestrator: %w", err)
	}
	defer requester.Close()

	response, err := requester.Request(ctx, tuiEvent(eventType, payload))
	if err != nil {
		return nil, err
	}
	return unwrapResult(response.Result)
}

// StreamChat sends a chat turn over the bridge's stream. Only a bridge the turn never reached gives
// streamed false; any other failure is the turn's, and it is not sent again.
func (c *OrchestratorClient) StreamChat(ctx context.Context, payload map[string]any, progress func(string)) (map[string]any, bool, error) {
	if c.Bridge == nil {
		return nil, false, nil
	}
	response, err := c.Bridge.Chat(ctx, tuiEvent("message.created", payload), progress)
	if errors.Is(err, cli.ErrBridgeMissing) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	result, err := unwrapResult(response.Result)
	return result, true, err
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
	// The conversation domain's own answers hold a "conversation" field, which is not a wrapper.
	if domain, _ := envelope["domain"].(string); domain == "conversation" {
		return inner, nil
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
