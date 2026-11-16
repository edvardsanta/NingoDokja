package vision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestService(t *testing.T, generate ollamaGenerate) (*Service, *httptest.Server) {
	t.Helper()
	imageHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shot.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG-BYTES"))
		case "/page.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>not an image</html>"))
		case "/huge.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(make([]byte, 64<<10))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(imageHost.Close)
	service := NewService("http://127.0.0.1:9",
		WithMaxBytes(32<<10),
		WithGenerate(generate),
	)
	service.fetch = imageHost.Client()
	return service, imageHost
}

func TestDescribeReturnsTheModelReading(t *testing.T) {
	var gotImages []string
	service, host := newTestService(t, func(_ context.Context, model, prompt string, images []string) (string, error) {
		if model != "minicpm-v" || !strings.Contains(prompt, "português") || !strings.Contains(prompt, "apenas a primeira ocorrência") || !strings.Contains(prompt, "caractere por caractere") {
			t.Fatal("model or prompt not applied", model)
		}
		gotImages = images
		return "  Grand Theft Auto: San Andreas — R$ 874,21. Captura da página da loja.  ", nil
	})
	text, err := service.Describe(context.Background(), DescribeRequest{
		URL: host.URL + "/shot.png", Filename: "shot.png", ContentType: "image/png", Size: 9,
	})
	if err != nil || !strings.Contains(text, "R$ 874,21") {
		t.Fatal(text, err)
	}
	if len(gotImages) != 1 {
		t.Fatal("image not sent to the model")
	}
	if decoded, err := base64.StdEncoding.DecodeString(gotImages[0]); err != nil || string(decoded) != "PNG-BYTES" {
		t.Fatal("image bytes not base64-encoded for ollama")
	}
}

func TestDescribeRejectsUnsafeOrOversizedInput(t *testing.T) {
	service, _ := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		t.Fatal("model called for invalid input")
		return "", nil
	})
	for _, request := range []DescribeRequest{
		{URL: "file:///etc/passwd"},
		{URL: "https://user:pass@example.com/a.png"},
		{URL: strings.Repeat("x", MaxURLBytes+1)},
		{URL: "https://example.com/a.png", Size: 32<<10 + 1},
	} {
		if _, err := service.Describe(context.Background(), request); !errors.Is(err, ErrInvalidRequest) && !errors.Is(err, ErrTooLarge) {
			t.Fatalf("unsafe input accepted: %v (%v)", request, err)
		}
	}
}

func TestDescribeBoundsDownloadAndRejectsNonImages(t *testing.T) {
	called := false
	service, host := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		called = true
		return "x", nil
	})
	if _, err := service.Describe(context.Background(), DescribeRequest{URL: host.URL + "/huge.png"}); !errors.Is(err, ErrTooLarge) {
		t.Fatal("oversized download accepted", err)
	}
	if _, err := service.Describe(context.Background(), DescribeRequest{URL: host.URL + "/page.html"}); !errors.Is(err, ErrFetchFailed) {
		t.Fatal("non-image accepted", err)
	}
	if _, err := service.Describe(context.Background(), DescribeRequest{URL: host.URL + "/missing.png"}); !errors.Is(err, ErrFetchFailed) {
		t.Fatal("missing image accepted", err)
	}
	if called {
		t.Fatal("model called for rejected downloads")
	}
	if _, err := service.Describe(context.Background(), DescribeRequest{URL: host.URL + "/shot.png", ContentType: "text/html"}); err != nil {
		t.Fatal("declared content type must not override the download", err)
	}
}

func TestDescribeReportsAnEmptyModelReading(t *testing.T) {
	service, host := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		return "  ", nil
	})
	if _, err := service.Describe(context.Background(), DescribeRequest{URL: host.URL + "/shot.png"}); err == nil {
		t.Fatal("empty reading accepted")
	}
}

func TestDescribeCapsLongReadings(t *testing.T) {
	service, host := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		return strings.Repeat("a", 5000), nil
	})
	text, err := service.Describe(context.Background(), DescribeRequest{URL: host.URL + "/shot.png"})
	if err != nil || len([]rune(text)) != 4001 {
		t.Fatal("reading not capped", len([]rune(text)), err)
	}
}

func TestServerDescribeAndHealth(t *testing.T) {
	service, host := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		return "uma captura de loja", nil
	})
	server := NewServer(service, "127.0.0.1", "0")
	api := httptest.NewServer(server.http.Handler)
	t.Cleanup(api.Close)

	health, err := http.Get(api.URL + "/health")
	if err != nil || health.StatusCode != http.StatusOK {
		t.Fatal("health failed", err)
	}
	encoded, _ := json.Marshal(DescribeRequest{URL: host.URL + "/shot.png", Filename: "shot.png", ContentType: "image/png"})
	response, err := http.Post(api.URL+"/describe", "application/json", strings.NewReader(string(encoded)))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal("describe failed", err, response.StatusCode)
	}
	var decoded map[string]any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil || decoded["text"] != "uma captura de loja" || decoded["model"] != "minicpm-v" {
		t.Fatal(decoded, err)
	}

	encoded, _ = json.Marshal(DescribeRequest{URL: "file:///etc/passwd"})
	response, err = http.Post(api.URL+"/describe", "application/json", strings.NewReader(string(encoded)))
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatal("invalid request accepted", err, response.StatusCode)
	}
	response, err = http.Post(api.URL+"/describe", "application/json", strings.NewReader("{broken"))
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatal("broken body accepted", err, response.StatusCode)
	}
}
