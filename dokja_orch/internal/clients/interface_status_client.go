package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

const (
	interfaceStatusTimeout = 3 * time.Second
	interfaceStatusMaxBody = 4 * 1024
	interfaceDetailMax     = 200
)

var interfaceStateName = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)

// InterfaceStatusClient reads the state an interface answers on its internal status listener:
// a level, a state from the interface's own closed list and a fixed sentence about it. The
// interface is trusted, but what it says is shown by an unauthenticated status request, so the
// reply is cut down to a short name and a short printable sentence.
type InterfaceStatusClient struct {
	endpoint   string
	httpClient *http.Client
}

// NewInterfaceStatusClient returns nil when there is no http(s) address: an interface that is not
// deployed has no endpoint, and is simply not listed.
func NewInterfaceStatusClient(endpoint string) *InterfaceStatusClient {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil
	}
	return &InterfaceStatusClient{endpoint: endpoint, httpClient: &http.Client{Timeout: interfaceStatusTimeout}}
}

// Status returns the level (ok, degraded or error), the state and the sentence.
func (c *InterfaceStatusClient) Status(ctx context.Context) (string, string, string, error) {
	if c == nil || c.httpClient == nil {
		return "", "", "", fmt.Errorf("interface status client is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("build interface status request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", "", "", fmt.Errorf("call interface status: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("interface status returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, interfaceStatusMaxBody))
	if err != nil {
		return "", "", "", fmt.Errorf("read interface status: %w", err)
	}
	var reply struct {
		Status string `json:"status"`
		State  string `json:"state"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", "", "", fmt.Errorf("decode interface status: %w", err)
	}

	level := strings.TrimSpace(reply.Status)
	if level != "ok" && level != "degraded" {
		level = "error"
	}
	state := strings.TrimSpace(reply.State)
	if !interfaceStateName.MatchString(state) {
		state = "unknown"
	}
	return level, state, printableSentence(reply.Detail), nil
}

// printableSentence keeps the visible characters of a short sentence and drops the rest.
func printableSentence(text string) string {
	var out []rune
	for _, r := range strings.TrimSpace(text) {
		if len(out) >= interfaceDetailMax {
			break
		}
		if unicode.IsPrint(r) {
			out = append(out, r)
		}
	}
	return string(out)
}
