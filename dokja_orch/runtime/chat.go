package runtime

import (
	"encoding/json"
	"net/http"
	domain "read_books/dokja_domain/dokja_runtime"
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
		if r.Method != "POST" || r.URL.Path != "/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Messages []domain.ChatMessage `json:"messages"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072)).Decode(&body) != nil {
			w.WriteHeader(http.StatusBadRequest)
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
