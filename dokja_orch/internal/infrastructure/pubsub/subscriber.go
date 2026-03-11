package pubsub

import "context"

type Subscriber interface {
	Subscribe(ctx context.Context, channel string, handler func(string)) error
	Close() error
}
