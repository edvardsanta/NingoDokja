package clients

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func visionServer(t *testing.T, seen *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/describe" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(seen); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"um gato no teclado","model":"minicpm-v"}`))
	}))
}

func TestVisionClientDescribesHandedOverBytes(t *testing.T) {
	var seen map[string]any
	server := visionServer(t, &seen)
	defer server.Close()
	client := NewVisionServiceClient(server.URL)
	text, err := client.DescribeMedia(context.Background(), base64.StdEncoding.EncodeToString([]byte("MP4-BYTES")), "video/mp4", "post-1.mp4")
	if err != nil || text != "um gato no teclado" {
		t.Fatal(text, err)
	}
	if seen["data_b64"] != base64.StdEncoding.EncodeToString([]byte("MP4-BYTES")) ||
		seen["content_type"] != "video/mp4" || seen["filename"] != "post-1.mp4" {
		t.Fatalf("describe payload mismatch: %v", seen)
	}
}

func TestVisionClientDownloadsAnImageItDescribes(t *testing.T) {
	image := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shot.png" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG-BYTES"))
	}))
	defer image.Close()
	var seen map[string]any
	server := visionServer(t, &seen)
	defer server.Close()
	client := NewVisionServiceClient(server.URL)
	text, err := client.DescribeImage(context.Background(), image.URL+"/shot.png", "shot.png", "", 9)
	if err != nil || text != "um gato no teclado" {
		t.Fatal(text, err)
	}
	if seen["data_b64"] != base64.StdEncoding.EncodeToString([]byte("PNG-BYTES")) ||
		seen["content_type"] != "image/png" {
		t.Fatalf("image bytes not handed over: %v", seen)
	}
}

func TestVisionClientFetchGuards(t *testing.T) {
	image := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/page.html" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>not media</html>"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG"))
	}))
	defer image.Close()
	var seen map[string]any
	server := visionServer(t, &seen)
	defer server.Close()
	client := NewVisionServiceClient(server.URL)
	if _, err := client.DescribeImage(context.Background(), "file:///etc/passwd", "x", "", 0); err == nil {
		t.Fatal("non-http address accepted")
	}
	if _, err := client.DescribeImage(context.Background(), image.URL+"/shot.png", "x", "", maxVisionMediaBytes+1); err == nil {
		t.Fatal("oversized media accepted")
	}
	if _, err := client.DescribeImage(context.Background(), image.URL+"/page.html", "x", "", 0); err == nil {
		t.Fatal("non-image download accepted")
	}
	if len(seen) != 0 {
		t.Fatalf("vision called for rejected input: %v", seen)
	}
}
