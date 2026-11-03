//go:build unix

package feeds

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const maxWebBytes = 2 << 20
const maxWebRunes = 80000
const webTimeout = 12 * time.Second

type webPassage struct {
	Text  string `json:"text"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type webReader struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

func newWebReader() webReader {
	return webReader{lookup: net.DefaultResolver.LookupIPAddr, dial: (&net.Dialer{Timeout: 4 * time.Second}).DialContext}
}

func webURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid web address")
	}
	if port := u.Port(); port != "" && !((u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80")) {
		return nil, fmt.Errorf("unsupported web port")
	}
	return u, nil
}

// Allow public destinations only. In particular, exclude metadata, shared address
// space, mapped private IPv4, transition mechanisms and special-purpose ranges.
func publicWebIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		if netip.MustParsePrefix(block).Contains(ip) {
			return false
		}
	}
	return ip.Is4() || netip.MustParsePrefix("2000::/3").Contains(ip)
}

// Resolve once per connection, reject mixed public/private answers, then connect
// to a validated literal IP. HTTP retains the original host for TLS verification.
func (r webReader) safeDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (port != "80" && port != "443") {
		return nil, fmt.Errorf("blocked destination")
	}
	ips, err := r.lookup(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("unavailable destination")
	}
	for _, candidate := range ips {
		ip, ok := netip.AddrFromSlice(candidate.IP)
		if !ok || !publicWebIP(ip) || candidate.Zone != "" {
			return nil, fmt.Errorf("blocked destination")
		}
	}
	for _, candidate := range ips {
		conn, dialErr := r.dial(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	return nil, err
}

func (s *Service) readWeb(ctx context.Context, payload map[string]any) (map[string]any, error) {
	raw, _ := payload["url"].(string)
	ctx, cancel := context.WithTimeout(ctx, webTimeout)
	defer cancel()
	select {
	case s.extractions <- struct{}{}:
		defer func() { <-s.extractions }()
	case <-ctx.Done():
		return nil, fmt.Errorf("web reading timed out")
	}
	client := newWebReader().client()
	defer client.CloseIdleConnections()
	return fetchWeb(ctx, client, raw, s.now())
}

func (r webReader) client() *http.Client {
	transport := &http.Transport{Proxy: nil, DialContext: r.safeDial, DisableKeepAlives: true, TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 6 * time.Second, MaxResponseHeaderBytes: 64 << 10}
	return &http.Client{Transport: transport, Timeout: webTimeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return fmt.Errorf("too many redirects")
		}
		_, err := webURL(req.URL.String())
		return err
	}}
}

// This reader never starts a browser, plugin, subprocess or page script and never
// fetches embedded resources. Its only network operation is a bounded HTTP GET.
func fetchWeb(ctx context.Context, client *http.Client, raw string, now time.Time) (map[string]any, error) {
	original, err := webURL(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, original.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid web request")
	}
	req.Header.Set("Accept", "text/html, application/xhtml+xml, text/plain")
	req.Header.Set("User-Agent", "DokjaTextReader/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web reading unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web reading unavailable (HTTP %d)", resp.StatusCode)
	}
	kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if kind != "text/html" && kind != "application/xhtml+xml" && kind != "text/plain" {
		return nil, fmt.Errorf("unsupported web content")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBytes+1))
	if err != nil || len(data) > maxWebBytes {
		return nil, fmt.Errorf("web content exceeds reading limit or is incomplete")
	}
	decoded, err := charset.NewReader(strings.NewReader(string(data)), resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, fmt.Errorf("unreadable web encoding")
	}
	decodedData, err := io.ReadAll(io.LimitReader(decoded, 3*maxWebBytes+1))
	if err != nil || len(decodedData) > 3*maxWebBytes {
		return nil, fmt.Errorf("unreadable web content")
	}
	final := *resp.Request.URL
	if final.Fragment == "" {
		final.Fragment = original.Fragment
	}
	title, published, passages := extractWeb(string(decodedData), kind, &final)
	if len(passages) == 0 {
		return nil, fmt.Errorf("web page has no readable text")
	}
	start := 0
	if final.Fragment != "" {
		for i, passage := range passages {
			u, _ := url.Parse(passage.URL)
			if u.Fragment == final.Fragment {
				start = i
				break
			}
		}
	}
	// Prefer the requested anchor, which may be far down a large statute.
	partial := start > 0
	passages = passages[start:]
	kept := make([]webPassage, 0, len(passages))
	remaining := maxWebRunes
	for _, passage := range passages {
		runes := []rune(passage.Text)
		if len(runes) > remaining {
			passage.Text = string(runes[:remaining])
			partial = true
		}
		kept = append(kept, passage)
		remaining -= len([]rune(passage.Text))
		if remaining == 0 {
			partial = true
			break
		}
	}
	if title == "" {
		title = final.Hostname()
	}
	return map[string]any{"url": final.String(), "title": cleanText(title, 300), "published": cleanText(published, 100), "retrieved_at": now.UTC().Format(time.RFC3339), "partial": partial, "passages": kept}, nil
}

var webArticle = regexp.MustCompile(`(?i)^Art\.?\s*\d+[\p{L}\p{N}º°.-]*`)
var webParagraph = regexp.MustCompile(`^(§\s*\d+[º°.-]*|Parágrafo único)`)

func extractWeb(text, kind string, base *url.URL) (title, published string, passages []webPassage) {
	if kind == "text/plain" {
		for i, line := range strings.Split(text, "\n\n") {
			if line = strings.TrimSpace(line); line != "" {
				runes := []rune(line)
				for len(runes) > 0 {
					end := min(2400, len(runes))
					passages = append(passages, webPassage{Text: string(runes[:end]), Label: fmt.Sprintf("Passage %d", i+1), URL: base.String()})
					runes = runes[end:]
				}
			}
		}
		return
	}
	root, err := html.Parse(strings.NewReader(text))
	if err != nil {
		return
	}
	hasAttr := func(n *html.Node, key string) bool {
		for _, a := range n.Attr {
			if a.Key == key {
				return true
			}
		}
		return false
	}
	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}
	var nodeText func(*html.Node) string
	nodeText = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(nodeText(c))
		}
		return b.String()
	}
	var main, article *html.Node
	var metadata func(*html.Node)
	metadata = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				title = strings.Join(strings.Fields(nodeText(n)), " ")
			case "main":
				if main == nil {
					main = n
				}
			case "article":
				if article == nil {
					article = n
				}
			case "meta":
				name := strings.ToLower(attr(n, "property"))
				if name == "" {
					name = strings.ToLower(attr(n, "name"))
				}
				if published == "" && (name == "article:published_time" || name == "datepublished" || name == "date") {
					published = attr(n, "content")
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			metadata(c)
		}
	}
	metadata(root)
	if article != nil {
		root = article
	} else if main != nil {
		root = main
	}
	var buf strings.Builder
	anchor, heading, articleLabel := "", "", ""
	flush := func() {
		value := strings.Join(strings.Fields(buf.String()), " ")
		buf.Reset()
		if value == "" {
			return
		}
		label := heading
		if match := webArticle.FindString(value); match != "" {
			articleLabel = match
			label = match
		}
		if match := webParagraph.FindString(value); match != "" {
			label = strings.TrimSpace(articleLabel + " " + match)
		}
		if label == "" {
			label = articleLabel
		}
		if label == "" {
			label = fmt.Sprintf("Passage %d", len(passages)+1)
		}
		address := *base
		address.Fragment = anchor
		// Bounded passages preserve order and the page's original anchor.
		runes := []rune(value)
		for len(runes) > 0 {
			end := min(2400, len(runes))
			passages = append(passages, webPassage{Text: string(runes[:end]), Label: cleanText(label, 200), URL: address.String()})
			runes = runes[end:]
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			buf.WriteString(n.Data)
			return
		}
		block := false
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "iframe", "object", "embed", "svg", "nav", "footer", "header", "form", "template", "head":
				return
			}
			if hasAttr(n, "hidden") || attr(n, "aria-hidden") == "true" {
				return
			}
			switch n.Data {
			case "p", "div", "section", "article", "main", "li", "blockquote", "br", "tr", "h1", "h2", "h3", "h4", "h5", "h6":
				block = true
				flush()
			}
			id := attr(n, "id")
			if id == "" && n.Data == "a" {
				id = attr(n, "name")
			}
			if id != "" {
				flush()
				anchor = id
			}
			if strings.HasPrefix(n.Data, "h") && len(n.Data) == 2 && n.Data[1] >= '1' && n.Data[1] <= '6' {
				heading = strings.Join(strings.Fields(nodeText(n)), " ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if block {
			flush()
		}
	}
	walk(root)
	flush()
	return
}
