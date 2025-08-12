package redis

import (
	"context"
	"github.com/go-redis/redis/v8"
)

type Publisher struct {
	client *redis.Client
}

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
