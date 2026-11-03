//go:build unix

package feeds

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type webRoundTrip func(*http.Request) (*http.Response, error)

func (f webRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebDestinations(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.100.100.200", "0.1.2.3", "192.168.1.1", "198.18.0.1", "224.0.0.1", "240.0.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "64:ff9b::a00:1", "2002:7f00:1::", "2001:db8::1"} {
		if publicWebIP(netip.MustParseAddr(raw)) {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicWebIP(netip.MustParseAddr(raw)) {
			t.Errorf("blocked public %s", raw)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "https://user:pass@example.com", "http://example.com:8080", "https://example.com:80"} {
		if _, err := webURL(raw); err == nil {
			t.Errorf("allowed %s", raw)
		}
	}
}

func TestWebDialPinsPublicIPAndRejectsMixedDNS(t *testing.T) {
	calls := 0
	r := webReader{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, dial: func(_ context.Context, _, address string) (net.Conn, error) {
		calls++
		if address != "8.8.8.8:443" {
			t.Errorf("unpinned destination: %s", address)
		}
		return nil, fmt.Errorf("test")
	}}
	_, _ = r.safeDial(context.Background(), "tcp", "example.com:443")
	if calls != 1 {
		t.Fatal("did not dial pinned IP")
	}
	r.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	_, _ = r.safeDial(context.Background(), "tcp", "example.com:443")
	if calls != 1 {
		t.Fatal("dialed mixed private/public DNS")
	}
}

func TestWebExtractKeepsLegalAnchorsAndIgnoresActiveContent(t *testing.T) {
	u, _ := url.Parse("https://example.com/law#art140")
	title, date, passages := extractWeb(`<html><head><title>Law</title><meta property="article:published_time" content="2026-01-01"></head><body><nav>menu</nav><p>Earlier text</p><p><a name="art140"></a>Art. 140. Estimate the financial impact.</p><p>§ 1º Include calculation assumptions.</p><script>fetch('https://other.example/secret')</script><iframe>hidden</iframe></body></html>`, "text/html", u)
	if title != "Law" || date != "2026-01-01" || len(passages) != 3 {
		t.Fatalf("%s %s %#v", title, date, passages)
	}
	if passages[1].URL != "https://example.com/law#art140" || !strings.Contains(passages[2].Label, "140") || strings.Contains(fmt.Sprint(passages), "fetch(") {
		t.Fatalf("lost anchor or retained active content: %#v", passages)
	}
}

func TestWebFetchBoundsContentAndPrioritizesFragment(t *testing.T) {
	client := &http.Client{Transport: webRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(`<p>Earlier text</p><p id="art140">Art. 140. Exact legal text.</p><p>§ 1º More text.</p>`)), Request: req}, nil
	})}
	result, err := fetchWeb(context.Background(), client, "https://example.com/law#art140", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	passages := result["passages"].([]webPassage)
	if !strings.HasPrefix(passages[0].Text, "Art. 140") || result["partial"] != true {
		t.Fatalf("%#v", result)
	}
	client.Transport = webRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxWebBytes+1))), Request: req}, nil
	})
	if _, err := fetchWeb(context.Background(), client, "https://example.com", time.Now()); err == nil {
		t.Fatal("accepted oversized response")
	}
}

func TestWebRedirectsCannotReachPrivateNetworks(t *testing.T) {
	for _, destination := range []string{"http://127.0.0.1/secrets", "http://internal.example/secrets", "http://example.com:8080/secrets", "file:///etc/passwd"} {
		t.Run(destination, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("reader sent credentials")
				}
				http.Redirect(w, r, destination, http.StatusFound)
			}))
			defer server.Close()
			dials := 0
			reader := webReader{lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
				ip := "8.8.8.8"
				if host != "example.com" {
					ip = "127.0.0.1"
				}
				return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
			}, dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				dials++
				return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
			}}
			client := reader.client()
			defer client.CloseIdleConnections()
			if _, err := fetchWeb(context.Background(), client, "http://example.com/page", time.Now()); err == nil {
				t.Fatal("accepted unsafe redirect")
			}
			if dials != 1 {
				t.Fatalf("private redirect dialed: %d connections", dials)
			}
		})
	}
}

func TestWebReaderNeverRequestsEmbeddedResources(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<article><h1>A page</h1><p>Visible text.</p><p hidden>Hidden instructions</p><script src="/evil.js"></script><img src="/secret"><iframe src="/frame"></iframe></article>`)
	}))
	defer server.Close()
	reader := webReader{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}, dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
	}}
	client := reader.client()
	defer client.CloseIdleConnections()
	result, err := fetchWeb(context.Background(), client, "http://example.com/page", time.Now())
	if err != nil || requests != 1 || strings.Contains(fmt.Sprint(result), "Hidden instructions") {
		t.Fatalf("requests=%d result=%#v err=%v", requests, result, err)
	}
}

func TestWebMainArticleBeatsRelatedStoryCards(t *testing.T) {
	u, _ := url.Parse("https://example.com/story")
	for _, page := range []string{
		`<main><h1>Main committee story</h1><section><article><h2>Unrelated tax headline</h2></article></section><div><p>The committee chair controls the meeting agenda.</p><p>The article explains the nomination procedure.</p></div><aside>Unrelated sidebar</aside></main>`,
		`<main><article><h2>Unrelated tax headline</h2></article><article><h1>Main committee story</h1><p>The committee chair controls the meeting agenda.</p></article></main>`,
		`<article><h2>Unrelated tax headline</h2></article><article><p>The committee chair controls the meeting agenda.</p></article>`,
		`<main><h1>Main committee story</h1><article><h2>Unrelated tax headline</h2></article><article><p>The committee chair controls the meeting agenda.</p></article></main>`,
	} {
		_, _, passages := extractWeb(page, "text/html", u)
		combined := fmt.Sprint(passages)
		if !strings.Contains(combined, "committee chair") || strings.Contains(combined, "Unrelated") {
			t.Fatalf("wrong article selected: %s", combined)
		}
	}
}
