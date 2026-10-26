package runtime

import (
	"encoding/json"
	"net/http"
	domain "read_books/dokja_domain/dokja_runtime"
	"sync"
	"time"
)

func ChatHandler(d *domain.Domain) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fail := func(err error) {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		}
		if r.Header.Get("Origin") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method == "GET" && r.URL.Path == "/health" {
			if err := d.ChatReady(); err != nil {
				fail(err)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"status": "configured"})
			return
		}
		if r.Method != "POST" || (r.URL.Path != "/chat" && r.URL.Path != "/chat/stream" && r.URL.Path != "/chat/measured") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/chat/measured" {
			serveMeasuredChat(w, r, d, fail)
			return
		}
		var body struct {
			Messages []domain.ChatMessage `json:"messages"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072)).Decode(&body) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/chat/stream" {
			streamChat(w, r, d, body.Messages, fail)
			return
		}
		result, err := d.Chat(r.Context(), body.Messages)
		if err != nil {
			fail(err)
			return
		}
		json.NewEncoder(w).Encode(result)
	})
}

// previewEvery is the least time between two previews: what the reader sees grows smoothly, and the
// line is not flooded token by token.
const previewEvery = 100 * time.Millisecond

// streamChat answers with one JSON object per line: {"preview": text so far} while the provider
// writes, then the reply as /chat gives it, or {"error": ...}. A failure before any preview is
// answered as /chat answers it. The previews are provisional; the last line is the answer.
func streamChat(w http.ResponseWriter, r *http.Request, d *domain.Domain, messages []domain.ChatMessage, fail func(error)) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, _ := w.(http.Flusher)
	var mu sync.Mutex
	started := false
	var last time.Time
	send := func(v any) {
		json.NewEncoder(w).Encode(v)
		if flusher != nil {
			flusher.Flush()
		}
	}
	result, err := d.ChatStream(r.Context(), messages, func(text string) {
		mu.Lock()
		defer mu.Unlock()
		if now := time.Now(); now.Sub(last) >= previewEvery {
			last = now
			started = true
			send(map[string]string{"preview": text})
		}
	})
	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		if !started {
			w.Header().Set("Content-Type", "application/json")
			fail(err)
			return
		}
		send(map[string]string{"error": err.Error()})
		return
	}
	send(result)
}
