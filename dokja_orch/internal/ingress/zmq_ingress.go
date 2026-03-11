package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"read_books/internal/core"
	"read_books/internal/infrastructure/zeromq"
	"read_books/internal/logger"
	"strings"
	"sync"
)

const (
	defaultZeroMQEventEndpoint = "tcp://127.0.0.1:5555"
	defaultZeroMQEventTopic    = "events"
)

type ZeroMQEventIngress struct {
	service    *core.Service
	endpoint   string
	topic      string
	subscriber *zeromq.Subscriber
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func NewZeroMQEventIngress(service *core.Service, endpoint, topic string) *ZeroMQEventIngress {
	return &ZeroMQEventIngress{
		service:  service,
		endpoint: firstNonEmpty(endpoint, os.Getenv("APP_QUEUE_ADDR"), defaultZeroMQEventEndpoint),
		topic:    firstNonEmpty(topic, os.Getenv("VA_ZMQ_TOPIC"), defaultZeroMQEventTopic),
	}
}

func (i *ZeroMQEventIngress) Name() string {
	return "va-orchestrator-ingress"
}

func (i *ZeroMQEventIngress) Start() error {
	if i == nil {
		return fmt.Errorf("zeromq ingress is nil")
	}
	if i.service == nil {
		return fmt.Errorf("orchestrator service is not configured")
	}

	subscriber, err := zeromq.NewBoundSubscriber(i.endpoint)
	if err != nil {
		return fmt.Errorf("create subscriber: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	i.mu.Lock()
	i.subscriber = subscriber
	i.cancel = cancel
	i.mu.Unlock()

	logger.Info(fmt.Sprintf("ZeroMQ ingress listening on %s topic %s", i.endpoint, i.topic))

	if err := subscriber.Subscribe(ctx, i.topic, func(message string) {
		var event core.Event
		if err := json.Unmarshal([]byte(message), &event); err != nil {
			logger.Error("Failed to decode orchestrator event", err)
			return
		}

		if err := i.service.Process(ctx, event); err != nil {
			logger.Error("Failed to process orchestrator event", err)
		}
	}); err != nil {
		cancel()
		_ = subscriber.Close()
		return fmt.Errorf("subscribe to topic %q: %w", i.topic, err)
	}

	return nil
}

func (i *ZeroMQEventIngress) Stop() error {
	if i == nil {
		return nil
	}

	i.mu.Lock()
	cancel := i.cancel
	subscriber := i.subscriber
	i.cancel = nil
	i.subscriber = nil
	i.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if subscriber != nil {
		return subscriber.Close()
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
