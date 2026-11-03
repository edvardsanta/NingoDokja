//go:build unix

package feeds

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"
)

//go:embed web_browser.py
var webBrowserScript string

type webBrowserFetch func(context.Context, *url.URL, netip.Addr) (*http.Response, error)

type webBrowserTransport struct {
	primary http.RoundTripper
	reader  webReader
}

func (t webBrowserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.primary.RoundTrip(req)
	if err != nil || t.reader.browser == nil || (response.StatusCode != 403 && response.StatusCode != 429 && !strings.EqualFold(response.Header.Get("Cf-Mitigated"), "challenge")) {
		return response, err
	}
	ips, err := t.reader.publicAddresses(req.Context(), req.URL.Hostname())
	if err != nil {
		return response, nil
	}
	ip, _ := netip.AddrFromSlice(ips[0].IP)
	fallback, err := t.reader.browser(req.Context(), req.URL, ip.Unmap())
	if err != nil {
		return response, nil
	}
	response.Body.Close()
	fallback.Request = req
	return fallback, nil
}

// Only the bundled helper code runs. It receives data on stdin, no shell,
// inherited credentials, proxy settings, site packages from the working tree,
// browser or JavaScript engine. Go owns DNS validation and all redirects.
func runWebBrowser(ctx context.Context, address *url.URL, ip netip.Addr) (*http.Response, error) {
	if !publicWebIP(ip) {
		return nil, fmt.Errorf("blocked destination")
	}
	python := "/opt/feeds-web/bin/python3"
	if _, err := os.Stat(python); err != nil {
		var lookupErr error
		python, lookupErr = exec.LookPath("python3")
		if lookupErr != nil {
			return nil, fmt.Errorf("browser client unavailable")
		}
	}
	timeout := webTimeout.Seconds()
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline).Seconds())
	}
	if timeout <= 0 {
		return nil, context.DeadlineExceeded
	}
	input, _ := json.Marshal(map[string]any{"url": address.String(), "ip": ip.String(), "timeout": timeout})
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &capped{limit: 3 << 20, onFull: cancel}
	cmd := exec.CommandContext(childCtx, python, "-I", "-c", webBrowserScript)
	cmd.Dir = os.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil || output.isFull() {
		return nil, fmt.Errorf("browser client unavailable")
	}
	var result struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Location    string `json:"location"`
		Challenge   string `json:"challenge"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(output.bytes(), &result); err != nil || result.Status < 100 || result.Status > 599 {
		return nil, fmt.Errorf("invalid browser result")
	}
	body, err := base64.StdEncoding.Strict().DecodeString(result.Body)
	if err != nil || len(body) > maxWebBytes || len(result.ContentType) > 1000 || len(result.Location) > 2048 || len(result.Challenge) > 100 {
		return nil, fmt.Errorf("invalid browser result")
	}
	response := &http.Response{StatusCode: result.Status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: &http.Request{URL: address}}
	response.Header.Set("Content-Type", result.ContentType)
	response.Header.Set("Location", result.Location)
	response.Header.Set("Cf-Mitigated", result.Challenge)
	return response, nil
}

func (t webBrowserTransport) CloseIdleConnections() {
	if closer, ok := t.primary.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
