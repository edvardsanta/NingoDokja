package clients

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type VoiceServiceClient struct {
	endpoint string
	http     *http.Client
}

func NewVoiceServiceClient(endpoint string) *VoiceServiceClient {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = "http://dokja-voice:8081"
	}
	return &VoiceServiceClient{strings.TrimRight(endpoint, "/"), &http.Client{Timeout: 15 * time.Second}}
}
func (c *VoiceServiceClient) Invoke(ctx context.Context, operation string, payload map[string]any) (map[string]any, error) {
	if operation != "tts" && operation != "stt" && operation != "wakeword" && operation != "activity" {
		return nil, fmt.Errorf("unsupported voice operation")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/"+operation, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice service unavailable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voice service returned status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 8*1024*1024+1))
	if err != nil || len(data) > 8*1024*1024 {
		return nil, fmt.Errorf("invalid voice response size")
	}
	if operation == "tts" {
		if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
			return nil, fmt.Errorf("invalid voice audio response")
		}
		return map[string]any{"audio_bytes_b64": base64.StdEncoding.EncodeToString(data), "audio_format": "wav"}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid voice response")
	}
	return result, nil
}
