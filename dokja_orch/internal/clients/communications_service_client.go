package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type CommunicationsServiceClient struct {
	endpoint, token string
	httpClient      *http.Client
}

func NewCommunicationsServiceClient(endpoint, token string) *CommunicationsServiceClient {
	return &CommunicationsServiceClient{endpoint: strings.TrimRight(endpoint, "/"), token: token,
		httpClient: &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *CommunicationsServiceClient) Dispatch(ctx context.Context, action string, payload map[string]any) (map[string]any, error) {
	if c.endpoint == "" {
		return nil, fmt.Errorf("communications service endpoint is not configured")
	}
	body, err := json.Marshal(map[string]any{"action": action, "payload": payload})
	if err != nil {
		return nil, fmt.Errorf("encode communications request")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/dispatch", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("invalid communications endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("communications service is unavailable")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, fmt.Errorf("communications reply is too large or unreadable")
	}
	var answer struct {
		Result map[string]any `json:"result"`
		Error  string         `json:"error"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("invalid communications reply")
	}
	if response.StatusCode != http.StatusOK || answer.Result == nil {
		switch answer.Error {
		case "permission_denied", "rate_limited", "not_configured", "channel_unavailable":
			return nil, fmt.Errorf("communications: %s", answer.Error)
		default:
			return nil, fmt.Errorf("communications service request failed")
		}
	}
	return answer.Result, nil
}
