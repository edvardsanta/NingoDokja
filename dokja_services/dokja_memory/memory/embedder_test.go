package memory

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaEmbedderSendsTheModelAndReturnsUnitVectors(t *testing.T) {
	var seen map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&seen)
		json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{3, 4}, {0, 2}}})
	}))
	defer server.Close()

	embedder := NewOllamaEmbedder(server.URL+"/", "some-model", time.Second)
	vectors, err := embedder.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}

	if seen["model"] != "some-model" || seen["truncate"] != true || len(seen["input"].([]any)) != 2 {
		t.Fatalf("request body %v", seen)
	}
	if math.Abs(float64(vectors[0][0])-0.6) > 1e-6 || math.Abs(float64(vectors[0][1])-0.8) > 1e-6 || vectors[1][1] != 1 {
		t.Fatalf("vectors must be normalized: %v", vectors)
	}
}

func TestOllamaEmbedderReportsEveryFailureAsUnavailable(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"an error status": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "model not found", http.StatusNotFound)
		},
		"a bad body": func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("not json")) },
		"the wrong number of vectors": func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1}}})
		},
		"an empty vector": func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{}, {}}})
		},
	}
	for name, handler := range cases {
		server := httptest.NewServer(handler)
		_, err := NewOllamaEmbedder(server.URL, "m", time.Second).Embed(context.Background(), []string{"a", "b"})
		server.Close()
		if !errors.Is(err, ErrEmbedUnavailable) {
			t.Fatalf("%s: expected ErrEmbedUnavailable, got %v", name, err)
		}
	}
}

func TestOllamaEmbedderGivesUpOnASlowServerAndOnNoServer(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(400 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()

	started := time.Now()
	_, err := NewOllamaEmbedder(slow.URL, "m", 100*time.Millisecond).Embed(context.Background(), []string{"a"})
	if !errors.Is(err, ErrEmbedUnavailable) || time.Since(started) > time.Second {
		t.Fatalf("a slow embedder must not hold the service: err=%v after %v", err, time.Since(started))
	}

	gone := httptest.NewServer(http.NotFoundHandler())
	url := gone.URL
	gone.Close()
	if _, err := NewOllamaEmbedder(url, "m", time.Second).Embed(context.Background(), []string{"a"}); !errors.Is(err, ErrEmbedUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if vectors, err := NewOllamaEmbedder(url, "m", time.Second).Embed(context.Background(), nil); err != nil || vectors != nil {
		t.Fatalf("nothing to embed is not an error: %v %v", vectors, err)
	}
}

func TestOllamaEmbedderReachable(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("reachability should only list models, got %s", r.URL.Path)
		}
	}))
	defer up.Close()
	if !NewOllamaEmbedder(up.URL, "m", time.Second).Reachable(context.Background()) {
		t.Fatal("a server that answers is reachable")
	}

	url := up.URL
	up.Close()
	if NewOllamaEmbedder(url, "m", time.Second).Reachable(context.Background()) {
		t.Fatal("a closed server is not reachable")
	}
}
