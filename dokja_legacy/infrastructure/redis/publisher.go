package redis

import (
	"context"
	"read_books/internal/legacy/infrastructure/pubsub"

	"github.com/go-redis/redis/v8"
)

type Publisher struct {
	client *redis.Client
}

var _ pubsub.Publisher = (*Publisher)(nil)

func NewPublisher(addr, password string, db int) *Publisher {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &Publisher{client: rdb}
}

func (p *Publisher) Publish(ctx context.Context, channel string, message string) error {
	return p.client.Publish(ctx, channel, message).Err()
}

func (p *Publisher) Close() error {
	return p.client.Close()
}
