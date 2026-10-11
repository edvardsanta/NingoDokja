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
)

const (
	interfaceLinkTimeout  = 30 * time.Second // a reset unlinks on WhatsApp's side first
	interfaceLinkMaxBody  = 64 * 1024
	interfaceLinkMaxSide  = 177 // the largest QR code, version 40
	interfaceLinkMinSide  = 21
	interfaceLinkRowChars = "01"
)

var interfaceLinkRow = regexp.MustCompile(`^[` + interfaceLinkRowChars + `]+$`)

// InterfaceLinkClient asks an interface that links to an account (WhatsApp) for the code to scan,
// for a new attempt, and to start over. It talks to the interface's internal listener, next to its
// status route. The code travels as the rows of its picture and is never logged here.
type InterfaceLinkClient struct {
	base       string
	httpClient *http.Client
}

// NewInterfaceLinkClient derives the link routes from the interface's status address, so there
// is one address to configure. It returns nil when there is none.
func NewInterfaceLinkClient(statusEndpoint string) *InterfaceLinkClient {
	endpoint := strings.TrimSpace(statusEndpoint)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil
	}
	base := strings.TrimSuffix(strings.TrimRight(endpoint, "/"), "/status")
	return &InterfaceLinkClient{base: base, httpClient: &http.Client{Timeout: interfaceLinkTimeout}}
}

// Status returns the state and, while the account waits for a link, the rows of the code.
func (c *InterfaceLinkClient) Status(ctx context.Context) (string, []string, error) {
	return c.call(ctx, http.MethodGet, "/link")
}

// Start asks for a new link attempt now.
func (c *InterfaceLinkClient) Start(ctx context.Context) (string, error) {
	state, _, err := c.call(ctx, http.MethodPost, "/link/start")
	return state, err
}

// Reset unlinks the account, erases the saved session and asks for a new code.
func (c *InterfaceLinkClient) Reset(ctx context.Context) (string, error) {
	state, _, err := c.call(ctx, http.MethodPost, "/link/reset")
	return state, err
}

func (c *InterfaceLinkClient) call(ctx context.Context, method, path string) (string, []string, error) {
	if c == nil || c.httpClient == nil {
		return "", nil, fmt.Errorf("interface link client is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return "", nil, fmt.Errorf("build interface link request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", nil, fmt.Errorf("call interface link: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("interface link returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, interfaceLinkMaxBody))
	if err != nil {
		return "", nil, fmt.Errorf("read interface link: %w", err)
	}
	var reply struct {
		State  string   `json:"state"`
		Matrix []string `json:"matrix"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", nil, fmt.Errorf("decode interface link: %w", err)
	}

	state := strings.TrimSpace(reply.State)
	if !interfaceStateName.MatchString(state) {
		state = "unknown"
	}
	matrix, err := checkedMatrix(reply.Matrix)
	if err != nil {
		return "", nil, err
	}
	return state, matrix, nil
}

// checkedMatrix accepts nothing but a square grid of 0 and 1 within the sizes a QR code has, so
// whatever draws it is handed data and never markup.
func checkedMatrix(rows []string) ([]string, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	side := len(rows)
	if side < interfaceLinkMinSide || side > interfaceLinkMaxSide {
		return nil, fmt.Errorf("interface link code has an unexpected size")
	}
	for _, row := range rows {
		if len(row) != side || !interfaceLinkRow.MatchString(row) {
			return nil, fmt.Errorf("interface link code is not a grid of modules")
		}
	}
	return rows, nil
}
