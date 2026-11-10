package runtime

import (
	"bytes"
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
	messages, binding, ok := decodeChatRequest(w, r)
	if !ok {
		return
	}
	var result domain.ChatReply
	var err error
	if binding.Provider != "" || binding.Model != "" {
		result, err = h.domain.ChatWithBinding(r.Context(), binding, messages)
	} else {
		result, err = h.domain.Chat(r.Context(), messages)
	}
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

func decodeChatRequest(w http.ResponseWriter, r *http.Request) ([]domain.ChatMessage, domain.ChatBinding, bool) {
	var body struct {
		Messages []domain.ChatMessage `json:"messages"`
		Provider string               `json:"provider"`
		Model    string               `json:"model"`
		// Format is absent (text), "json", or a JSON Schema object; only an explicit
		// provider/model binding may ask for it.
		Format json.RawMessage `json:"format"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072)).Decode(&body) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return nil, domain.ChatBinding{}, false
	}
	format := ""
	if raw := bytes.TrimSpace(body.Format); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var named string
		if json.Unmarshal(raw, &named) == nil {
			format = named
		} else {
			var compact bytes.Buffer
			if json.Compact(&compact, raw) != nil {
				w.WriteHeader(http.StatusBadRequest)
				return nil, domain.ChatBinding{}, false
			}
			format = compact.String()
		}
	}
	if format != "" && body.Provider == "" && body.Model == "" {
		w.WriteHeader(http.StatusBadRequest)
		return nil, domain.ChatBinding{}, false
	}
	return body.Messages, domain.ChatBinding{Provider: body.Provider, Model: body.Model, Format: format}, true
}

func decodeChatMessages(w http.ResponseWriter, r *http.Request) ([]domain.ChatMessage, bool) {
	messages, _, ok := decodeChatRequest(w, r)
	return messages, ok
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
