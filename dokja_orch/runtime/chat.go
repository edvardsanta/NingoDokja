package runtime

import (
	"encoding/json"
	"net/http"
	domain "read_books/dokja_domain/dokja_runtime"
	"sync"
	"time"
)

func ChatHandler(d *domain.Domain) http.Handler {
	h := inferenceHTTPHandler{domain: d}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /chat", h.chat)
	mux.HandleFunc("POST /chat/stream", h.stream)
	mux.HandleFunc("POST /chat/measured", h.measured)
	mux.HandleFunc("POST /math/render", renderMath)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Origin") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type inferenceHTTPHandler struct {
	domain *domain.Domain
}

func (h inferenceHTTPHandler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.domain.ChatReady(); err != nil {
		writeInferenceError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "configured"})
}

func (h inferenceHTTPHandler) chat(w http.ResponseWriter, r *http.Request) {
	messages, ok := decodeChatMessages(w, r)
	if !ok {
		return
	}
	result, err := h.domain.Chat(r.Context(), messages)
	if err != nil {
		writeInferenceError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (h inferenceHTTPHandler) stream(w http.ResponseWriter, r *http.Request) {
	messages, ok := decodeChatMessages(w, r)
	if !ok {
		return
	}
	fail := func(err error) {
		writeInferenceError(w, http.StatusServiceUnavailable, err.Error())
	}
	streamChat(w, r, h.domain, messages, fail)
}

func (h inferenceHTTPHandler) measured(w http.ResponseWriter, r *http.Request) {
	fail := func(err error) {
		writeInferenceError(w, http.StatusServiceUnavailable, err.Error())
	}
	serveMeasuredChat(w, r, h.domain, fail)
}

func decodeChatMessages(w http.ResponseWriter, r *http.Request) ([]domain.ChatMessage, bool) {
	var body struct {
		Messages []domain.ChatMessage `json:"messages"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072)).Decode(&body) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, false
	}
	return body.Messages, true
}

func writeInferenceError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
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
