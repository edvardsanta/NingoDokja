package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/pebbe/zmq4"
)

const defaultPublishWarmup = 200 * time.Millisecond

type Publisher struct {
	socket *zmq4.Socket
}

func NewPublisher(endpoint string) (*Publisher, error) {
	socket, err := zmq4.NewSocket(zmq4.PUB)
	if err != nil {
		return nil, err
	}

	if err := socket.Connect(endpoint); err != nil {
		socket.Close()
		return nil, err
	}

	return &Publisher{socket: socket}, nil
}

func (p *Publisher) Close() error {
	if p == nil || p.socket == nil {
		return nil
	}
	return p.socket.Close()
}

func (p *Publisher) PublishEvent(ctx context.Context, topic string, event Event) error {
	if p == nil || p.socket == nil {
		return fmt.Errorf("publisher is not configured")
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(defaultPublishWarmup):
	}

	if _, err := p.socket.Send(topic, zmq4.SNDMORE); err != nil {
		return err
	}
	_, err = p.socket.Send(string(body), 0)
	return err
}
