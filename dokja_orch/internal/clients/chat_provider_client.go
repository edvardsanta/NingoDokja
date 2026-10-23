package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

type ChatProviderClient struct{ client *http.Client }

func NewChatProviderClient(socket string) *ChatProviderClient {
	if socket == "" {
		socket = os.Getenv("DOKJA_INFERENCE_SOCKET")
	}
	if socket == "" {
		socket = "../.dokja/inference/chat.sock"
	}
	return &ChatProviderClient{client: &http.Client{Timeout: 95 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *ChatProviderClient) call(ctx context.Context, method, path string, body any) (map[string]any, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("invalid chat request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://inference"+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat inference is unavailable; start or restart the local runtime controller")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 524289))
	if err != nil || len(raw) > 524288 {
		return nil, fmt.Errorf("invalid chat provider response")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil {
		return nil, fmt.Errorf("invalid chat provider response")
	}
	if resp.StatusCode != 200 {
		if message, ok := result["error"].(string); ok {
			return nil, fmt.Errorf("%s", message)
		}
		return nil, fmt.Errorf("chat inference failed")
	}
	return result, nil
}
func (c *ChatProviderClient) Generate(ctx context.Context, message string) (map[string]any, error) {
	return c.GenerateMessages(ctx, []map[string]string{{"role": "user", "content": message}})
}
func (c *ChatProviderClient) GenerateMessages(ctx context.Context, messages []map[string]string) (map[string]any, error) {
	return c.call(ctx, "POST", "/chat", map[string]any{"messages": messages})
}
func (c *ChatProviderClient) Health(ctx context.Context) error {
	_, err := c.call(ctx, "GET", "/health", nil)
	return err
}
