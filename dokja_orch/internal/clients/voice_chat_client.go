package clients

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type VoiceChatClient struct {
	endpoint string
	client   *http.Client
}

func NewVoiceChatClient(endpoint string) *VoiceChatClient {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "http://dokja-voice:8081"
	}
	return &VoiceChatClient{endpoint: strings.TrimRight(endpoint, "/"), client: &http.Client{Timeout: 95 * time.Second}}
}

func (c *VoiceChatClient) GenerateVoiceMessagesStream(ctx context.Context, messages []map[string]string, onText func(string)) (map[string]any, error) {
	data, err := json.Marshal(map[string]any{"messages": messages})
	if err != nil {
		return nil, fmt.Errorf("invalid voice chat request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat/stream", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("local voice chat service is unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("local voice chat returned status %d", resp.StatusCode)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 16<<10), 128<<10)
	var reply strings.Builder
	for scanner.Scan() {
		var frame struct {
			Text  string `json:"text"`
			Done  bool   `json:"done"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return nil, fmt.Errorf("invalid local voice chat frame")
		}
		if frame.Error != "" {
			return nil, fmt.Errorf("%s", frame.Error)
		}
		if frame.Text != "" {
			if reply.Len()+len(frame.Text) > 16384 {
				return nil, fmt.Errorf("local voice reply is too long")
			}
			reply.WriteString(frame.Text)
			if onText != nil {
				onText(reply.String())
			}
		}
		if frame.Done {
			return map[string]any{"reply": reply.String()}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read local voice response: %w", err)
	}
	return nil, fmt.Errorf("local voice response ended before completion")
}
