package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pebbe/zmq4"
)

const DefaultRequestEndpoint = "tcp://127.0.0.1:5558"

// pollInterval is how often a pending request notices that its caller gave up.
const pollInterval = 100 * time.Millisecond

type Requester struct {
	socket *zmq4.Socket
}

type RequestResponse struct {
	Status string      `json:"status"`
	Result interface{} `json:"result"`
	Error  string      `json:"error"`
}

func NewRequester(endpoint string) (*Requester, error) {
	socket, err := zmq4.NewSocket(zmq4.REQ)
	if err != nil {
		return nil, err
	}
	if err := socket.Connect(endpoint); err != nil {
		socket.Close()
		return nil, err
	}
	return &Requester{socket: socket}, nil
}

func (r *Requester) Close() error {
	if r == nil || r.socket == nil {
		return nil
	}
	return r.socket.Close()
}

func (r *Requester) Request(ctx context.Context, event Event) (RequestResponse, error) {
	if r == nil || r.socket == nil {
		return RequestResponse{}, fmt.Errorf("requester is not configured")
	}

	body, err := json.Marshal(event)
	if err != nil {
		return RequestResponse{}, fmt.Errorf("marshal event: %w", err)
	}

	// One goroutine uses the socket from send to receive, and the caller closes it afterwards. A
	// receive on another goroutine that outlives a cancelled caller would race its Close, and libzmq
	// aborts the process when a socket is closed while another thread is still receiving on it.
	if _, err := r.socket.Send(string(body), 0); err != nil {
		return RequestResponse{}, err
	}
	poller := zmq4.NewPoller()
	poller.Add(r.socket, zmq4.POLLIN)
	for {
		if err := ctx.Err(); err != nil {
			return RequestResponse{}, err
		}
		ready, err := poller.Poll(pollInterval)
		if err != nil {
			return RequestResponse{}, err
		}
		if len(ready) > 0 {
			break
		}
	}
	raw, err := r.socket.Recv(0)
	if err != nil {
		return RequestResponse{}, err
	}
	var response RequestResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return RequestResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}
	if response.Status != "ok" {
		if response.Error == "" {
			response.Error = "orchestrator request failed"
		}
		return RequestResponse{}, errors.New(response.Error)
	}
	return response, nil
}
