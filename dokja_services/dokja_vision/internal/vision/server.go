package vision

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// maxDescribeBody bounds a /describe request: base64 inflates bytes by 4/3, and the
// largest handed-over media is a video of maxVideoBytes.
func maxDescribeBody(maxBytes int64) int64 {
	limit := int64(maxVideoBytes)
	if maxBytes > limit {
		limit = maxBytes
	}
	return base64Inflation(limit) + 4096
}

func base64Inflation(n int64) int64 {
	return (n + 2) / 3 * 4
}

// Server exposes the vision service over HTTP. It has no authentication: it
// answers only inside the compose network, like the other private services.
type Server struct {
	service *Service
	http    *http.Server
}

func NewServer(service *Service, host string, port string) *Server {
	mux := http.NewServeMux()
	server := &Server{service: service}
	mux.HandleFunc("POST /describe", server.describe)
	mux.HandleFunc("GET /health", server.health)
	server.http = &http.Server{
		Addr:              host + ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      100 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	return server
}

func (server *Server) Addr() string { return server.http.Addr }

func (server *Server) ListenAndServe() error {
	return server.http.ListenAndServe()
}

func (server *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"status": "ok", "model": server.service.Model()})
}

func (server *Server) describe(response http.ResponseWriter, request *http.Request) {
	limit := maxDescribeBody(server.service.maxBytes)
	body, err := io.ReadAll(io.LimitReader(request.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		writeError(response, http.StatusBadRequest, "invalid request body")
		return
	}
	var describeRequest DescribeRequest
	if err := json.Unmarshal(body, &describeRequest); err != nil {
		writeError(response, http.StatusBadRequest, "invalid request body")
		return
	}
	text, err := server.service.Describe(request.Context(), describeRequest)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidRequest):
			writeError(response, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrTooLarge):
			writeError(response, http.StatusRequestEntityTooLarge, err.Error())
		case errors.Is(err, ErrFetchFailed):
			writeError(response, http.StatusBadGateway, err.Error())
		default:
			writeError(response, http.StatusServiceUnavailable, "vision model unavailable")
		}
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"text": text, "model": server.service.Model()})
}

func writeJSON(response http.ResponseWriter, status int, value map[string]any) {
	encoded, _ := json.Marshal(value)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_, _ = response.Write(encoded)
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]any{"error": message})
}
