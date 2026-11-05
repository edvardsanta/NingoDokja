//go:build unix

package feeds

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed web_pdf.py
var webPDFScript string

type pdfPage struct {
	Page int    `json:"page"`
	Text string `json:"text"`
}
type pdfReading struct {
	Title   string    `json:"title"`
	Partial bool      `json:"partial"`
	Pages   []pdfPage `json:"pages"`
}
type webPDFRead func(context.Context, []byte, int) (pdfReading, error)

func runWebPDF(ctx context.Context, data []byte, page int) (pdfReading, error) {
	var result pdfReading
	python, err := exec.LookPath("python3")
	if err != nil {
		return result, fmt.Errorf("PDF reader unavailable")
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	output := &capped{limit: 1 << 20, onFull: cancel}
	cmd := exec.CommandContext(childCtx, python, "-I", "-c", webPDFScript, strconv.Itoa(page))
	cmd.Dir = os.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = output
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil || output.isFull() {
		if ctx.Err() != nil {
			return result, fmt.Errorf("web_timeout: PDF reading deadline exceeded")
		}
		return result, fmt.Errorf("web_pdf_unreadable: PDF text extraction failed")
	}
	if err := json.Unmarshal(output.bytes(), &result); err != nil || len(result.Pages) == 0 || len(result.Pages) > 500 {
		return result, fmt.Errorf("web_pdf_unreadable: invalid PDF reading")
	}
	return result, nil
}

func readWebPDF(ctx context.Context, data []byte, address *url.URL, now time.Time, read webPDFRead) (map[string]any, error) {
	start := 1
	if strings.HasPrefix(address.Fragment, "page=") {
		value, err := strconv.Atoi(strings.TrimPrefix(address.Fragment, "page="))
		if err != nil || value < 1 {
			return nil, fmt.Errorf("web_pdf_unreadable: invalid page")
		}
		start = value
	}
	reading, err := read(ctx, data, start)
	if err != nil {
		return nil, err
	}
	var passages []webPassage
	remaining := maxWebRunes
	partial := reading.Partial || start > 1
	previous := 0
	for _, page := range reading.Pages {
		if page.Page < start || page.Page <= previous {
			return nil, fmt.Errorf("web_pdf_unreadable: invalid page order")
		}
		previous = page.Page
		text := []rune(strings.TrimSpace(strings.ToValidUTF8(page.Text, "")))
		if len(text) > remaining {
			text = text[:remaining]
			partial = true
		}
		pageURL := *address
		pageURL.Fragment = fmt.Sprintf("page=%d", page.Page)
		remaining -= len(text)
		for len(text) > 0 {
			end := min(2400, len(text))
			passages = append(passages, webPassage{Text: string(text[:end]), Label: fmt.Sprintf("Page %d", page.Page), URL: pageURL.String()})
			text = text[end:]
		}
		if remaining == 0 {
			partial = true
			break
		}
	}
	if len(passages) == 0 {
		return nil, fmt.Errorf("web_pdf_unreadable: no extractable text")
	}
	title := cleanText(reading.Title, 300)
	if title == "" {
		title = cleanText(path.Base(address.Path), 300)
	}
	return map[string]any{"url": address.String(), "title": title, "published": "", "retrieved_at": now.UTC().Format(time.RFC3339), "partial": partial, "passages": passages}, nil
}
