package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pebbe/zmq4"
)

const DefaultRequestEndpoint = "tcp://127.0.0.1:5558"

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

	errCh := make(chan error, 1)
	responseCh := make(chan RequestResponse, 1)

	go func() {
		if _, sendErr := r.socket.Send(string(body), 0); sendErr != nil {
			errCh <- sendErr
			return
		}

		raw, recvErr := r.socket.Recv(0)
		if recvErr != nil {
			errCh <- recvErr
			return
		}

		var response RequestResponse
		if unmarshalErr := json.Unmarshal([]byte(raw), &response); unmarshalErr != nil {
			errCh <- fmt.Errorf("unmarshal response: %w", unmarshalErr)
			return
		}
		responseCh <- response
	}()

	select {
	case <-ctx.Done():
		return RequestResponse{}, ctx.Err()
	case err := <-errCh:
		return RequestResponse{}, err
	case response := <-responseCh:
		if response.Status != "ok" {
			if response.Error == "" {
				response.Error = "orchestrator request failed"
			}
			return RequestResponse{}, errors.New(response.Error)
		}
		return response, nil
	}
}
