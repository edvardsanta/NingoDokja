package pubsub

import (
	"context"
)

type Publisher interface {
	Publish(ctx context.Context, channel string, message string) error
	Close() error
}
