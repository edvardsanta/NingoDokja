package clients

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
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
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
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

func (c *ChatProviderClient) RenderMath(ctx context.Context, expression string) ([]byte, error) {
	result, err := c.call(ctx, http.MethodPost, "/math/render", map[string]string{"expression": expression})
	if err != nil {
		return nil, err
	}
	encoded, _ := result["data_b64"].(string)
	contentType, _ := result["content_type"].(string)
	if contentType != "image/png" || len(encoded) > 350000 {
		return nil, fmt.Errorf("invalid formula image")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) > 256<<10 {
		return nil, fmt.Errorf("invalid formula image")
	}
	return data, nil
}

// GenerateMessagesStream is GenerateMessages with a preview: the runtime's /chat/stream sends the
// text written so far, then the reply. A runtime without /chat/stream is asked through /chat, with no
// preview. A preview is shown and thrown away; only the returned reply is the answer.
func (c *ChatProviderClient) GenerateMessagesStream(ctx context.Context, messages []map[string]string, onPreview func(string)) (map[string]any, error) {
	data, err := json.Marshal(map[string]any{"messages": messages})
	if err != nil {
		return nil, fmt.Errorf("invalid chat request")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://inference/chat/stream", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("chat inference is unavailable; start or restart the local runtime controller")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return c.GenerateMessages(ctx, messages)
	}
	if resp.StatusCode != http.StatusOK {
		var failure map[string]any
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 524289))
		if json.Unmarshal(raw, &failure) == nil {
			if message, ok := failure["error"].(string); ok {
				return nil, fmt.Errorf("%s", message)
			}
		}
		return nil, fmt.Errorf("chat inference failed")
	}
	// Each preview repeats the text so far, so a long reply streams far more than its own size: lines
	// are bounded, and the request's deadline bounds the stream.
	lines := bufio.NewScanner(resp.Body)
	lines.Buffer(make([]byte, 0, 64<<10), 524288)
	for lines.Scan() {
		var line map[string]any
		if json.Unmarshal(lines.Bytes(), &line) != nil {
			return nil, fmt.Errorf("invalid chat provider response")
		}
		if preview, ok := line["preview"].(string); ok {
			if onPreview != nil {
				onPreview(preview)
			}
			continue
		}
		if message, ok := line["error"].(string); ok {
			return nil, fmt.Errorf("%s", message)
		}
		return line, nil
	}
	return nil, fmt.Errorf("invalid chat provider response")
}

func (c *ChatProviderClient) Health(ctx context.Context) error {
	_, err := c.call(ctx, "GET", "/health", nil)
	return err
}

// GenerateMeasuredMessages requests result acceptance limits, not provider-side
// generation or billing caps. Runtime reports counts or withholds the reply.
func (c *ChatProviderClient) GenerateMeasuredMessages(ctx context.Context, messages []map[string]string, budget map[string]int64) (map[string]any, error) {
	return c.call(ctx, "POST", "/chat/measured", map[string]any{"messages": messages, "budget": budget})
}
