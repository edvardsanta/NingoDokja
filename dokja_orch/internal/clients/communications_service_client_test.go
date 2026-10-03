package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommunicationsClientAuthenticatesAndProjectsErrors(t *testing.T) {
	mode := "ok"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.URL.Path != "/dispatch" {
			t.Error("missing service authentication")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request["action"] != "history" {
			t.Error("invalid service request")
		}
		if mode == "ok" {
			_, _ = w.Write([]byte(`{"result":{"messages":[]}}`))
			return
		}
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": mode})
	}))
	defer server.Close()
	client := NewCommunicationsServiceClient(server.URL, "test-secret")
	result, err := client.Dispatch(context.Background(), "history", map[string]any{"channel_id": "123"})
	if err != nil || result["messages"] == nil {
		t.Fatal("history failed", err)
	}
	mode = "rate_limited"
	if _, err = client.Dispatch(context.Background(), "history", nil); err == nil || !strings.Contains(err.Error(), "rate_limited") {
		t.Fatal("missing rate limit", err)
	}
	mode = "private upstream content"
	if _, err = client.Dispatch(context.Background(), "history", nil); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("unsafe error", err)
	}
}
func TestCommunicationsClientNeverFollowsRedirects(t *testing.T) {
	reached := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer source.Close()
	_, err := NewCommunicationsServiceClient(source.URL, "secret").Dispatch(context.Background(), "channels", nil)
	if err == nil || reached {
		t.Fatal("credential followed a redirect")
	}
}
