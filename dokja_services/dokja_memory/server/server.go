// Package server exposes the memory service over ZeroMQ REQ/REP, with the same JSON envelope
// as the knowledge service: {"type", "payload"} in, {"status", "result" | "message"} out.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"syscall"
	"time"

	"github.com/pebbe/zmq4"
)

const (
	pollInterval   = 500 * time.Millisecond
	requestTimeout = 30 * time.Second
)

// Handler is the service behind the socket.
type Handler interface {
	Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

type request struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

type okResponse struct {
	Status string         `json:"status"`
	Result map[string]any `json:"result"`
}

type errorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// HandleRequest turns one raw request into one raw response. It never panics and never
// returns without an answer, so the REP socket always gets its reply. Requests are logged
// by type and payload size only: a payload is the user's own text.
func HandleRequest(ctx context.Context, raw string, handler Handler, logger *log.Logger) (reply []byte) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Printf("request panicked: %v", recovered)
			reply = failure("internal error")
		}
	}()

	var parsed request
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return failure("request is not valid JSON")
	}
	if parsed.Type == "" {
		return failure("request must contain 'type'")
	}
	if parsed.Payload == nil {
		parsed.Payload = map[string]any{}
	}
	logger.Printf("handling type=%s payload_keys=%d", parsed.Type, len(parsed.Payload))

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	result, err := handler.Dispatch(ctx, parsed.Type, parsed.Payload)
	if err != nil {
		logger.Printf("request failed type=%s: %v", parsed.Type, err)
		return failure(err.Error())
	}
	if result == nil {
		result = map[string]any{}
	}
	body, err := json.Marshal(okResponse{Status: "ok", Result: result})
	if err != nil {
		return failure("failed to encode response")
	}
	return body
}

func failure(message string) []byte {
	body, _ := json.Marshal(errorResponse{Status: "error", Message: message})
	return body
}

// Serve binds a REP socket at endpoint and answers requests one at a time until ctx ends.
func Serve(ctx context.Context, endpoint string, handler Handler, logger *log.Logger) error {
	zctx, err := zmq4.NewContext()
	if err != nil {
		return fmt.Errorf("create zmq context: %w", err)
	}
	defer zctx.Term()

	socket, err := zctx.NewSocket(zmq4.REP)
	if err != nil {
		return fmt.Errorf("create zmq socket: %w", err)
	}
	defer socket.Close()

	// A receive timeout lets the loop notice that ctx has ended.
	if err := socket.SetRcvtimeo(pollInterval); err != nil {
		return fmt.Errorf("set receive timeout: %w", err)
	}
	if err := socket.Bind(endpoint); err != nil {
		return fmt.Errorf("bind %s: %w", endpoint, err)
	}
	logger.Printf("listening endpoint=%s", endpoint)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		raw, err := socket.Recv(0)
		if err != nil {
			if zmq4.AsErrno(err) == zmq4.Errno(syscall.EAGAIN) {
				continue
			}
			return fmt.Errorf("receive: %w", err)
		}
		if _, err := socket.SendBytes(HandleRequest(ctx, raw, handler, logger), 0); err != nil {
			return fmt.Errorf("send: %w", err)
		}
	}
}
