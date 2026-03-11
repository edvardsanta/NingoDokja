package repreq

import "io"

type RequesterReply interface {
	Request(message string) (string, error)
	io.Closer
}
