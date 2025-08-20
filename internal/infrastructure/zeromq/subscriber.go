package zeromq

import (
	"context"
	"github.com/pebbe/zmq4"
	"read_books/internal/infrastructure/pubsub"
)

type Subscriber struct {
	socket *zmq4.Socket
}

var _ pubsub.Subscriber = (*Subscriber)(nil)

func NewSubscriber(endpoint string) (*Subscriber, error) {
	socket, err := zmq4.NewSocket(zmq4.SUB)
	if err != nil {
		return nil, err
	}

	err = socket.Connect(endpoint)
	if err != nil {
		socket.Close()
		return nil, err
	}

	return &Subscriber{socket: socket}, nil
}

func (s *Subscriber) Subscribe(ctx context.Context, channel string, handler func(string)) error {
	err := s.socket.SetSubscribe(channel)
	if err != nil {
		return err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				_, err := s.socket.Recv(0)
				if err != nil {
					continue
				}

				message, err := s.socket.Recv(0)
				if err != nil {
					continue
				}
				handler(message)
			}
		}
	}()

	return nil
}

func (s *Subscriber) Close() error {
	return s.socket.Close()
}
