package redis

import (
	"context"
	"read_books/internal/legacy/infrastructure/pubsub"

	"github.com/go-redis/redis/v8"
)

type Subscriber struct {
	client *redis.Client
}

var _ pubsub.Subscriber = (*Subscriber)(nil)

func NewSubscriber(addr, password string, db int) *Subscriber {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &Subscriber{client: rdb}
}

func (s *Subscriber) Subscribe(ctx context.Context, channel string, handler func(string)) error {
	pubsub := s.client.Subscribe(ctx, channel)
	ch := pubsub.Channel()
	for msg := range ch {
		handler(msg.Payload)
	}
	return nil
}

func (s *Subscriber) Close() error {
	return s.client.Close()
}
