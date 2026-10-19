package zeromq

import (
	"read_books/internal/infrastructure/repreq"
	"time"

	"github.com/pebbe/zmq4"
)

type Requester struct {
	socket *zmq4.Socket
}

var _ repreq.RequesterReply = (*Requester)(nil)

func NewRequester(endpoint string) (*Requester, error) {
	return NewRequesterWithTimeout(endpoint, 5*time.Second)
}

// NewRequesterWithTimeout is NewRequester for a request that may take longer than the
// default 5 s to send or answer, such as an on-demand reading.
func NewRequesterWithTimeout(endpoint string, timeout time.Duration) (*Requester, error) {
	socket, err := zmq4.NewSocket(zmq4.REQ)
	if err != nil {
		return nil, err
	}

	if err := socket.Connect(endpoint); err != nil {
		socket.Close()
		return nil, err
	}

	socket.SetRcvtimeo(timeout)
	socket.SetSndtimeo(timeout)

	return &Requester{socket: socket}, nil
}

func (r *Requester) Request(message string) (string, error) {
	if _, err := r.socket.Send(message, 0); err != nil {
		return "", err
	}

	reply, err := r.socket.Recv(0)
	if err != nil {
		return "", err
	}

	return reply, nil
}

func (r *Requester) Close() error {
	return r.socket.Close()
}
