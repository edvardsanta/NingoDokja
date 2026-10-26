package runtime

import (
	"encoding/json"
	"io"
	"net/http"
	domain "read_books/dokja_domain/dokja_runtime"
)

func serveMeasuredChat(w http.ResponseWriter, r *http.Request, d *domain.Domain, fail func(error)) {
	var body struct {
		Messages []domain.ChatMessage `json:"messages"`
		Budget   domain.TokenBudget   `json:"budget"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 131072))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	result, err := d.ChatMeasured(r.Context(), body.Messages, body.Budget)
	if err != nil {
		fail(err)
		return
	}
	json.NewEncoder(w).Encode(result)
}
