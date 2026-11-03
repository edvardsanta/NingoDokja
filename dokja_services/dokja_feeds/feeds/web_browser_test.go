//go:build unix

package feeds

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestWebBrowserHelperSecurity(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	cmd := exec.Command(python, "-I", "-m", "unittest", "discover", "-s", ".", "-p", "test_web_browser.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", output, err)
	}
}

func TestWebBrowserFallbackPinsPublicDNSAndReturnsToRedirectGate(t *testing.T) {
	calls := 0
	reader := webReader{lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host != "example.com" {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, browser: func(_ context.Context, u *url.URL, ip netip.Addr) (*http.Response, error) {
		calls++
		if ip.String() != "8.8.8.8" || u.Hostname() != "example.com" {
			t.Fatal("fallback lost pin")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://internal.example/secret"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}
	transport := webBrowserTransport{reader: reader, primary: webRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() != "example.com" {
			return nil, fmt.Errorf("blocked destination")
		}
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("challenge")), Request: req}, nil
	})}
	client := reader.client()
	client.Transport = transport
	if _, err := fetchWeb(context.Background(), client, "http://example.com/story", time.Now()); err == nil {
		t.Fatal("redirect bypassed validation")
	}
	if calls != 1 {
		t.Fatalf("fallback called %d times", calls)
	}
	reader.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.1.2.3")}}, nil
	}
	transport.reader = reader
	client.Transport = transport
	_, _ = fetchWeb(context.Background(), client, "http://example.com/story", time.Now())
	if calls != 1 {
		t.Fatal("mixed private/public DNS reached fallback")
	}
}

func TestWebBlockedResponsesNeverBecomeEvidence(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		client := &http.Client{Transport: webRoundTrip(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("challenge tokens must stay private")), Request: req}, nil
		})}
		_, err := fetchWeb(context.Background(), client, "https://example.com/story", time.Now())
		if err == nil || !strings.HasPrefix(err.Error(), "web_blocked:") || strings.Contains(err.Error(), "tokens") {
			t.Fatalf("%v", err)
		}
	}
}
