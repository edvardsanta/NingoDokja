package zeromq

import (
	"context"
	"read_books/internal/infrastructure/pubsub"

	"github.com/pebbe/zmq4"
)

type Subscriber struct {
	socket *zmq4.Socket
}

var _ pubsub.Subscriber = (*Subscriber)(nil)

func NewSubscriber(endpoint string) (*Subscriber, error) {
	return newSubscriber(endpoint, false)
}

func NewBoundSubscriber(endpoint string) (*Subscriber, error) {
	return newSubscriber(endpoint, true)
}

func newSubscriber(endpoint string, bind bool) (*Subscriber, error) {
	socket, err := zmq4.NewSocket(zmq4.SUB)
	if err != nil {
		return nil, err
	}

	if bind {
		err = socket.Bind(endpoint)
	} else {
		err = socket.Connect(endpoint)
	}
	if err != nil {
		socket.Close()
		return nil, err
	}

	return &Subscriber{socket: socket}, nil
}

func (s *Subscriber) Subscribe(ctx context.Context, channel string, handler func(string)) error {
	if err := s.socket.SetSubscribe(channel); err != nil {
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
