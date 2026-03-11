package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"

	"github.com/pebbe/zmq4"
)

const defaultZeroMQRequestEndpoint = "tcp://*:5558"

type ZeroMQRequestIngress struct {
	service  *core.Service
	endpoint string
	context  *zmq4.Context
	socket   *zmq4.Socket
}

type RequestResponse struct {
	Status string      `json:"status"`
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func NewZeroMQRequestIngress(service *core.Service, endpoint string) *ZeroMQRequestIngress {
	return &ZeroMQRequestIngress{
		service:  service,
		endpoint: firstNonEmpty(endpoint, os.Getenv("VA_ZMQ_REQUEST_ENDPOINT"), defaultZeroMQRequestEndpoint),
	}
}

type CompactProcessResult struct {
	EventID  string      `json:"event_id"`
	Workflow string      `json:"workflow,omitempty"`
	Domain   string      `json:"domain,omitempty"`
	Result   interface{} `json:"result,omitempty"`
}

func (i *ZeroMQRequestIngress) Name() string {
	return "va-orchestrator-request-ingress"
}

func (i *ZeroMQRequestIngress) Start() error {
	if i == nil {
		return fmt.Errorf("zeromq request ingress is nil")
	}
	if i.service == nil {
		return fmt.Errorf("orchestrator service is not configured")
	}

	contextHandle, err := zmq4.NewContext()
	if err != nil {
		return fmt.Errorf("create zmq context: %w", err)
	}

	socket, err := contextHandle.NewSocket(zmq4.REP)
	if err != nil {
		contextHandle.Term()
		return fmt.Errorf("create zmq socket: %w", err)
	}

	if err := socket.Bind(i.endpoint); err != nil {
		socket.Close()
		contextHandle.Term()
		return fmt.Errorf("bind request socket: %w", err)
	}

	i.context = contextHandle
	i.socket = socket

	go i.serve()
	return nil
}

func (i *ZeroMQRequestIngress) serve() {
	for {
		raw, err := i.socket.Recv(0)
		if err != nil {
			return
		}

		response := i.handle(raw)
		body, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			body = []byte(`{"status":"error","error":"failed to encode response"}`)
		}
		_, _ = i.socket.Send(string(body), 0)
	}
}

func (i *ZeroMQRequestIngress) handle(raw string) RequestResponse {
	var event core.Event
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return RequestResponse{Status: "error", Error: fmt.Sprintf("decode request: %v", err)}
	}
	logger.Info(fmt.Sprintf(
		"request ingress received event_id=%s type=%s source=%s user=%s channel=%s payload_keys=%d",
		event.EventID,
		event.Type,
		event.Source,
		event.User.ID,
		event.Channel.ID,
		len(event.Payload),
	))

	result, err := i.service.ProcessWithResult(context.Background(), event)
	if err != nil {
		logger.Error(fmt.Sprintf("request ingress failed event_id=%s type=%s", event.EventID, event.Type), err)
		return RequestResponse{Status: "error", Error: err.Error()}
	}
	logger.Info(fmt.Sprintf(
		"request ingress completed event_id=%s workflow=%s domains=%d",
		result.Event.EventID,
		result.Workflow,
		len(result.Domains),
	))

	return RequestResponse{Status: "ok", Result: buildResponsePayload(result)}
}

func buildResponsePayload(result core.ProcessResult) interface{} {
	if verboseResponsesEnabled() {
		return result
	}

	compact := CompactProcessResult{
		EventID:  result.Event.EventID,
		Workflow: result.Workflow,
	}

	if len(result.Domains) == 1 {
		compact.Domain = result.Domains[0]
		if domainResult, ok := result.Result[result.Domains[0]]; ok {
			compact.Result = domainResult
			return compact
		}
	}

	compact.Result = result.Result
	return compact
}

func verboseResponsesEnabled() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("VA_RESPONSE_MODE")))
	return mode == "debug" || mode == "verbose"
}

func (i *ZeroMQRequestIngress) Stop() error {
	if i == nil {
		return nil
	}

	var errParts []string
	if i.socket != nil {
		if err := i.socket.Close(); err != nil {
			errParts = append(errParts, err.Error())
		}
		i.socket = nil
	}
	if i.context != nil {
		if err := i.context.Term(); err != nil {
			errParts = append(errParts, err.Error())
		}
		i.context = nil
	}
	if len(errParts) > 0 {
		return fmt.Errorf("%s", strings.Join(errParts, "; "))
	}
	return nil
}
