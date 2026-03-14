package container

import (
	pubsub2 "read_books/internal/dokja_legacy/infrastructure/pubsub"
	redis2 "read_books/internal/dokja_legacy/infrastructure/redis"
	zeromq2 "read_books/internal/dokja_legacy/infrastructure/zeromq"
	"read_books/internal/legacy/infrastructure/repreq"

	"go.uber.org/dig"
)

// Container provides dependency injection for the application
type Container struct {
	container *dig.Container
}

// NewContainer creates and initializes a new dependency injection container
func NewContainer() *Container {
	c := dig.New()
	return &Container{
		container: c,
	}
}

// ProvideRedisPublisher registers a Redis publisher in the container
func (c *Container) ProvideRedisPublisher(addr, password string, db int) {
	c.container.Provide(func() pubsub2.Publisher {
		return redis2.NewPublisher(addr, password, db)
	})
}

// ProvideRedisSubscriber registers a Redis subscriber in the container
func (c *Container) ProvideRedisSubscriber(addr, password string, db int) {
	c.container.Provide(func() pubsub2.Subscriber {
		return redis2.NewSubscriber(addr, password, db)
	})
}

// ProvideZeroMQPublisher registers a ZeroMQ publisher in the container
func (c *Container) ProvideZeroMQPublisher(endpoint string) error {
	return c.container.Provide(func() (pubsub2.Publisher, error) {
		return zeromq2.NewPublisher(endpoint)
	})
}

// ProvideZeroMQSubscriber registers a ZeroMQ subscriber in the container
func (c *Container) ProvideZeroMQSubscriber(endpoint string) error {
	return c.container.Provide(func() (pubsub2.Subscriber, error) {
		return zeromq2.NewSubscriber(endpoint)
	})
}

// ProvideZeroMQRequester registers a ZeroMQ requester in the container
func (c *Container) ProvideZeroMQRequester(endpoint string) error {
	return c.container.Provide(func() (repreq.RequesterReply, error) {
		return zeromq2.NewRequester(endpoint)
	})
}

// Invoke calls the given function with resolved dependencies
func (c *Container) Invoke(function interface{}) error {
	return c.container.Invoke(function)
}
