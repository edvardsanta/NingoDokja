// Package server exposes the feeds service over ZeroMQ to REQ clients, with the same JSON
// envelope as the knowledge service: {"type", "payload"} in, {"status", "result" |
// "message"} out.
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
	// pollInterval is also the longest a finished reply waits to be sent.
	pollInterval   = 10 * time.Millisecond
	requestTimeout = 30 * time.Second
	// extractTimeout bounds an on-demand reading, which runs a plugin while the caller waits.
	extractTimeout = 2 * time.Minute
	// classifyTimeout bounds a classification of documents already read (ADR 0008). It is
	// shorter than a reading: the caller waits for both before storing.
	classifyTimeout = time.Minute
	// maxInFlight bounds the requests being answered at once; past it a request is refused.
	maxInFlight = 16
)

// timeoutFor is how long one request may take.
func timeoutFor(eventType string) time.Duration {
	switch eventType {
	case "feeds.extract":
		return extractTimeout
	case "feeds.classify":
		return classifyTimeout
	}
	return requestTimeout
}

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

	ctx, cancel := context.WithTimeout(ctx, timeoutFor(parsed.Type))
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

// Serve binds a ROUTER socket at endpoint and answers requests until ctx ends. Each request
// is answered on its own goroutine, so an on-demand reading that waits on a plugin never
// delays feeds.status or feeds.items. The socket stays with this loop: replies come back
// over a channel and are sent from here.
func Serve(ctx context.Context, endpoint string, handler Handler, logger *log.Logger) error {
	zctx, err := zmq4.NewContext()
	if err != nil {
		return fmt.Errorf("create zmq context: %w", err)
	}
	defer zctx.Term()

	socket, err := zctx.NewSocket(zmq4.ROUTER)
	if err != nil {
		return fmt.Errorf("create zmq socket: %w", err)
	}
	defer socket.Close()
	// Replies to a client that went away are dropped instead of kept at shutdown.
	if err := socket.SetLinger(0); err != nil {
		return fmt.Errorf("set linger: %w", err)
	}
	if err := socket.Bind(endpoint); err != nil {
		return fmt.Errorf("bind %s: %w", endpoint, err)
	}
	logger.Printf("listening endpoint=%s", endpoint)

	poller := zmq4.NewPoller()
	poller.Add(socket, zmq4.POLLIN)
	replies := make(chan [][]byte, maxInFlight)
	slots := make(chan struct{}, maxInFlight)

	for {
		if err := sendReplies(socket, replies); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		polled, err := poller.Poll(pollInterval)
		if err != nil {
			if zmq4.AsErrno(err) == zmq4.Errno(syscall.EINTR) {
				continue
			}
			return fmt.Errorf("poll: %w", err)
		}
		if len(polled) == 0 {
			continue
		}
		frames, err := socket.RecvMessageBytes(0)
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}
		envelope, body, ok := split(frames)
		if !ok {
			logger.Printf("dropped a message that is not a REQ request")
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			if err := send(socket, append(envelope, failure("the feeds service is busy"))); err != nil {
				return err
			}
			continue
		}
		go func() {
			defer func() { <-slots }()
			reply := HandleRequest(ctx, string(body), handler, logger)
			select {
			case replies <- append(envelope, reply):
			case <-ctx.Done():
			}
		}()
	}
}

// split separates a REQ client's routing envelope (identities and the empty delimiter) from
// the request body.
func split(frames [][]byte) ([][]byte, []byte, bool) {
	last := len(frames) - 1
	if last < 2 || len(frames[last-1]) != 0 {
		return nil, nil, false
	}
	envelope := make([][]byte, last)
	copy(envelope, frames[:last])
	return envelope, frames[last], true
}

func sendReplies(socket *zmq4.Socket, replies chan [][]byte) error {
	for {
		select {
		case reply := <-replies:
			if err := send(socket, reply); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

func send(socket *zmq4.Socket, frames [][]byte) error {
	parts := make([]any, len(frames))
	for i, frame := range frames {
		parts[i] = frame
	}
	if _, err := socket.SendMessage(parts...); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}
