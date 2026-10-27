package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
	"sync"

	"github.com/pebbe/zmq4"
)

const defaultZeroMQRequestEndpoint = "tcp://*:5558"

// requestWorkers is how many requests the ingress answers at once. One slow request (a chat turn
// can take a minute) used to hold every other one behind it; now it holds one worker.
const requestWorkers = 8

type ZeroMQRequestIngress struct {
	service  *core.Service
	endpoint string
	context  *zmq4.Context
	// done closes when the proxy and every worker have closed their sockets
	done chan struct{}
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
	// Clients keep their REQ sockets: a ROUTER answers them the same way, and hands each request
	// to the next free worker through a DEALER.
	frontend, err := contextHandle.NewSocket(zmq4.ROUTER)
	if err != nil {
		contextHandle.Term()
		return fmt.Errorf("create zmq socket: %w", err)
	}
	frontend.SetLinger(0)
	if err := frontend.Bind(i.endpoint); err != nil {
		frontend.Close()
		contextHandle.Term()
		return fmt.Errorf("bind request socket: %w", err)
	}
	backend, err := contextHandle.NewSocket(zmq4.DEALER)
	if err != nil {
		frontend.Close()
		contextHandle.Term()
		return fmt.Errorf("create zmq socket: %w", err)
	}
	backend.SetLinger(0)
	workers := fmt.Sprintf("inproc://dokja-request-workers-%p", i)
	if err := backend.Bind(workers); err != nil {
		frontend.Close()
		backend.Close()
		contextHandle.Term()
		return fmt.Errorf("bind request workers: %w", err)
	}

	i.context = contextHandle
	i.done = make(chan struct{})
	var running sync.WaitGroup
	for n := 0; n < requestWorkers; n++ {
		running.Add(1)
		go func() {
			defer running.Done()
			i.work(contextHandle, workers)
		}()
	}
	running.Add(1)
	go func() {
		defer running.Done()
		// Returns when the context ends; each socket is closed by the goroutine that used it.
		_ = zmq4.Proxy(frontend, backend, nil)
		frontend.Close()
		backend.Close()
	}()
	go func() {
		running.Wait()
		close(i.done)
	}()
	return nil
}

// work answers requests one after another on its own socket. ZeroMQ sockets are not shared between
// goroutines, so each worker creates, uses and closes its own.
func (i *ZeroMQRequestIngress) work(contextHandle *zmq4.Context, workers string) {
	socket, err := contextHandle.NewSocket(zmq4.REP)
	if err != nil {
		return
	}
	defer socket.Close()
	socket.SetLinger(0)
	if err := socket.Connect(workers); err != nil {
		return
	}
	for {
		raw, err := socket.Recv(0)
		if err != nil {
			return
		}
		response := i.handle(raw)
		body, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			body = []byte(`{"status":"error","error":"failed to encode response"}`)
		}
		if _, err := socket.Send(string(body), 0); err != nil {
			return
		}
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
	if i == nil || i.context == nil {
		return nil
	}
	// Ending the context wakes every blocked socket; their goroutines close them, which Term
	// waits for.
	err := i.context.Term()
	i.context = nil
	if i.done != nil {
		<-i.done
	}
	if err != nil {
		return fmt.Errorf("%s", err.Error())
	}
	return nil
}
