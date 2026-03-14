package repreq

import "io"

type RequesterReply interface {
	Request(message string) (string, error)

	// Close closes the underlying resources
	io.Closer
}
