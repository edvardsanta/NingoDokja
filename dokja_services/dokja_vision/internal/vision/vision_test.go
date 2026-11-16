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

func newTestService(t *testing.T, generate ollamaGenerate) *Service {
	t.Helper()
	return NewService("http://127.0.0.1:9",
		WithMaxBytes(32<<10),
		WithGenerate(generate),
		WithPrompt("prompt de teste em português"),
	)
}

func imageRequest(t *testing.T) DescribeRequest {
	t.Helper()
	return DescribeRequest{
		DataB64:     base64.StdEncoding.EncodeToString([]byte("PNG-BYTES")),
		Filename:    "shot.png",
		ContentType: "image/png",
	}
}

func TestDescribeRefusesToRunWithoutAPrompt(t *testing.T) {
	service := NewService("http://127.0.0.1:9")
	if _, err := service.Describe(context.Background(), imageRequest(t)); !errors.Is(err, ErrNoPrompt) {
		t.Fatal("describe ran without a configured prompt", err)
	}
}

func TestDescribeReturnsTheModelReading(t *testing.T) {
	var gotImages []string
	service := newTestService(t, func(_ context.Context, model, prompt string, images []string) (string, error) {
		if model != "minicpm-v" || prompt != "prompt de teste em português" {
			t.Fatal("model or prompt not applied", model)
		}
		gotImages = images
		return "  Grand Theft Auto: San Andreas — R$ 874,21. Captura da página da loja.  ", nil
	})
	text, err := service.Describe(context.Background(), imageRequest(t))
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

func TestDescribeSamplesAVideoIntoFrames(t *testing.T) {
	stub := func(_ context.Context, _ string, _ string, images []string) (string, error) {
		if len(images) != 2 {
			t.Fatalf("frames not sampled together: %d", len(images))
		}
		for index, frame := range images {
			if decoded, err := base64.StdEncoding.DecodeString(frame); err != nil || string(decoded) != "FRAME-"+strings.Repeat("x", index+1) {
				t.Fatal("frame bytes mangled", index)
			}
		}
		return "o reel mostra um gato no teclado", nil
	}
	original := extractFrames
	extractFrames = func(_ context.Context, data []byte) ([]string, error) {
		if string(data) != "MP4-BYTES" {
			t.Fatal("video bytes not handed to the sampler")
		}
		return []string{
			base64.StdEncoding.EncodeToString([]byte("FRAME-x")),
			base64.StdEncoding.EncodeToString([]byte("FRAME-xx")),
		}, nil
	}
	t.Cleanup(func() { extractFrames = original })

	service := newTestService(t, stub)
	text, err := service.Describe(context.Background(), DescribeRequest{
		DataB64:     base64.StdEncoding.EncodeToString([]byte("MP4-BYTES")),
		Filename:    "post-1.mp4",
		ContentType: "video/mp4",
	})
	if err != nil || !strings.Contains(text, "gato no teclado") {
		t.Fatal(text, err)
	}
}

func TestDescribeRejectsUnsafeOrOversizedInput(t *testing.T) {
	service := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		t.Fatal("model called for invalid input")
		return "", nil
	})
	oversized := base64.StdEncoding.EncodeToString(make([]byte, 32<<10+1))
	for name, request := range map[string]DescribeRequest{
		"notBase64":        {DataB64: "!!!", ContentType: "image/png"},
		"empty":            {DataB64: base64.StdEncoding.EncodeToString(nil), ContentType: "image/png"},
		"oversized":        {DataB64: oversized, ContentType: "image/png"},
		"wrongKind":        {DataB64: base64.StdEncoding.EncodeToString([]byte("x")), ContentType: "text/html"},
		"missingKind":      {DataB64: base64.StdEncoding.EncodeToString([]byte("x"))},
		"unsupportedVideo": {DataB64: base64.StdEncoding.EncodeToString([]byte("x")), ContentType: "video/webm"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Describe(context.Background(), request); !errors.Is(err, ErrInvalidRequest) && !errors.Is(err, ErrTooLarge) {
				t.Fatalf("unsafe input accepted: %v", err)
			}
		})
	}
}

func TestDescribeReportsAnEmptyModelReading(t *testing.T) {
	service := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		return "  ", nil
	})
	if _, err := service.Describe(context.Background(), imageRequest(t)); err == nil {
		t.Fatal("empty reading accepted")
	}
}

func TestDescribeCapsLongReadings(t *testing.T) {
	service := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		return strings.Repeat("a", 5000), nil
	})
	text, err := service.Describe(context.Background(), imageRequest(t))
	if err != nil || len([]rune(text)) != 4001 {
		t.Fatal("reading not capped", len([]rune(text)), err)
	}
}

func TestServerDescribeAndHealth(t *testing.T) {
	service := newTestService(t, func(context.Context, string, string, []string) (string, error) {
		return "uma captura de loja", nil
	})
	server := NewServer(service, "127.0.0.1", "0")
	api := httptest.NewServer(server.http.Handler)
	t.Cleanup(api.Close)

	health, err := http.Get(api.URL + "/health")
	if err != nil || health.StatusCode != http.StatusOK {
		t.Fatal("health failed", err)
	}
	encoded, _ := json.Marshal(imageRequest(t))
	response, err := http.Post(api.URL+"/describe", "application/json", strings.NewReader(string(encoded)))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal("describe failed", err, response.StatusCode)
	}
	var decoded map[string]any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil || decoded["text"] != "uma captura de loja" || decoded["model"] != "minicpm-v" {
		t.Fatal(decoded, err)
	}

	encoded, _ = json.Marshal(DescribeRequest{DataB64: "!!!", ContentType: "image/png"})
	response, err = http.Post(api.URL+"/describe", "application/json", strings.NewReader(string(encoded)))
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatal("invalid request accepted", err, response.StatusCode)
	}
	response, err = http.Post(api.URL+"/describe", "application/json", strings.NewReader("{broken"))
	if err != nil || response.StatusCode != http.StatusBadRequest {
		t.Fatal("broken body accepted", err, response.StatusCode)
	}
}
