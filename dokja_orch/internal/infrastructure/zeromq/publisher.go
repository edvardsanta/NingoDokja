package zeromq

import (
	"context"
	"read_books/internal/infrastructure/pubsub"

	"github.com/pebbe/zmq4"
)

type Publisher struct {
	socket *zmq4.Socket
}

var _ pubsub.Publisher = (*Publisher)(nil)

func NewPublisher(endpoint string) (*Publisher, error) {
	socket, err := zmq4.NewSocket(zmq4.PUB)
	if err != nil {
		return nil, err
	}

	if err := socket.Bind(endpoint); err != nil {
		socket.Close()
		return nil, err
	}

	return &Publisher{socket: socket}, nil
}

func (p *Publisher) Publish(ctx context.Context, channel string, message string) error {
	_, err := p.socket.Send(channel, zmq4.SNDMORE)
	if err != nil {
		return err
	}

	_, err = p.socket.Send(message, 0)
	return err
}

func (p *Publisher) Close() error {
	return p.socket.Close()
}
