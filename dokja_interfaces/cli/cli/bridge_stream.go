package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
)

// ErrBridgeMissing means nothing reached the orchestrator: its bridge is not listening, or it has no
// chat stream. Only then may a send go over the request socket instead, since it was never sent.
var ErrBridgeMissing = errors.New("the orchestrator's bridge has no chat stream here")

// The longest line the stream may send: a final answer with its reply and sources.
const maxStreamLine = 4 << 20

// BridgeStream sends a chat turn to the orchestrator's HTTP bridge, which says where the send stands
// while it waits, the thing the request socket cannot do. It is the same stream Talk uses.
type BridgeStream struct {
	url    string
	token  string
	client *http.Client
}

// BridgeFromEnv reads the bridge the desktop uses, DOKJA_BRIDGE_ENDPOINT and its credential
// DOKJA_GATEWAY_INGRESS_TOKEN. Without both, or for an address off this machine, there is none: the
// credential is never sent to another host.
func BridgeFromEnv() (*BridgeStream, bool) {
	return NewBridgeStream(os.Getenv("DOKJA_BRIDGE_ENDPOINT"), os.Getenv("DOKJA_GATEWAY_INGRESS_TOKEN"), nil)
}

// NewBridgeStream answers false for a missing credential or an address that is not local.
func NewBridgeStream(endpoint, token string, client *http.Client) (*BridgeStream, bool) {
	token = strings.TrimSpace(token)
	base, err := url.Parse(strings.TrimSpace(endpoint))
	if token == "" || err != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, false
	}
	switch base.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return nil, false
	}
	if client == nil {
		client = http.DefaultClient
	}
	stream := base.ResolveReference(&url.URL{Path: "/orchestrator/chat/stream"})
	return &BridgeStream{url: stream.String(), token: token, client: client}, true
}

// Chat sends the turn and gives back the answer the request socket would give. progress hears
// "queued" and "dispatched" once each, as they change; the text written so far is not used here.
func (b *BridgeStream) Chat(ctx context.Context, event Event, progress func(state string)) (RequestResponse, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return RequestResponse{}, fmt.Errorf("marshal event: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.url, bytes.NewReader(body))
	if err != nil {
		return RequestResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+b.token)
	response, err := b.client.Do(request)
	if err != nil {
		if errors.Is(err, syscall.ECONNREFUSED) {
			return RequestResponse{}, ErrBridgeMissing
		}
		return RequestResponse{}, fmt.Errorf("the orchestrator's bridge did not answer: %w", err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return RequestResponse{}, ErrBridgeMissing
	case response.StatusCode == http.StatusUnauthorized:
		return RequestResponse{}, errors.New("the orchestrator's bridge refused the credential")
	case response.StatusCode != http.StatusOK:
		return RequestResponse{}, fmt.Errorf("the orchestrator's bridge answered %d", response.StatusCode)
	}

	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64<<10), maxStreamLine)
	last := ""
	for scanner.Scan() {
		var line struct {
			Progress string `json:"progress"`
			RequestResponse
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			return RequestResponse{}, fmt.Errorf("the orchestrator's bridge sent an unreadable line: %w", err)
		}
		switch {
		case line.Progress == "queued" || line.Progress == "dispatched":
			if line.Progress != last && progress != nil {
				progress(line.Progress)
			}
			last = line.Progress
		case line.Status != "":
			if line.Status != "ok" {
				return RequestResponse{}, fmt.Errorf("orchestrator error: %s", line.Error)
			}
			return line.RequestResponse, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return RequestResponse{}, fmt.Errorf("the chat stream broke off: %w", err)
	}
	return RequestResponse{}, errors.New("the chat stream ended without an answer")
}
