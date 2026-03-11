package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pebbe/zmq4"
)

const (
	defaultEventEndpoint       = "tcp://127.0.0.1:5555"
	defaultTopic               = "events"
	defaultRefreshInterval     = 45 * time.Minute
	defaultDispatchInterval    = 6 * time.Hour
	defaultBootstrapGrace      = 15 * time.Second
	defaultRefreshMaxItems     = 20
	defaultDispatchMemeLimit   = 1
	defaultPublishSocketWarmup = 200 * time.Millisecond
)

type Event struct {
	EventID   string         `json:"event_id"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
	Type      string         `json:"type"`
	User      EventUser      `json:"user"`
	Channel   EventChannel   `json:"channel"`
	Payload   map[string]any `json:"payload"`
	Context   map[string]any `json:"context"`
}

type EventUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EventChannel struct {
	ID string `json:"id"`
}

type Service struct {
	logger            *log.Logger
	eventEndpoint     string
	topic             string
	refreshInterval   time.Duration
	dispatchInterval  time.Duration
	bootstrapGrace    time.Duration
	refreshMaxItems   int
	dispatchMemeLimit int
	publisherFactory  func() (*zmq4.Socket, error)
}

func NewFromEnv(logger *log.Logger) (*Service, error) {
	if logger == nil {
		logger = log.New(os.Stdout, "[dokja-scheduler] ", log.LstdFlags|log.LUTC)
	}

	refreshInterval, err := durationFromEnv("DOKJA_SCHEDULER_MEME_REFRESH_INTERVAL", defaultRefreshInterval)
	if err != nil {
		return nil, err
	}
	dispatchInterval, err := durationFromEnv("DOKJA_SCHEDULER_MEME_DISPATCH_INTERVAL", defaultDispatchInterval)
	if err != nil {
		return nil, err
	}
	bootstrapGrace, err := durationFromEnv("DOKJA_SCHEDULER_MEME_BOOTSTRAP_GRACE_PERIOD", defaultBootstrapGrace)
	if err != nil {
		return nil, err
	}

	refreshMaxItems, err := intFromEnv("DOKJA_SCHEDULER_MEME_REFRESH_MAX_ITEMS", defaultRefreshMaxItems)
	if err != nil {
		return nil, err
	}
	dispatchMemeLimit, err := intFromEnv("DOKJA_SCHEDULER_MEME_DISPATCH_LIMIT", defaultDispatchMemeLimit)
	if err != nil {
		return nil, err
	}

	eventEndpoint := firstNonEmpty(os.Getenv("DOKJA_SCHEDULER_EVENT_ENDPOINT"), defaultEventEndpoint)
	topic := firstNonEmpty(os.Getenv("DOKJA_SCHEDULER_TOPIC"), defaultTopic)

	return &Service{
		logger:            logger,
		eventEndpoint:     eventEndpoint,
		topic:             topic,
		refreshInterval:   refreshInterval,
		dispatchInterval:  dispatchInterval,
		bootstrapGrace:    bootstrapGrace,
		refreshMaxItems:   refreshMaxItems,
		dispatchMemeLimit: dispatchMemeLimit,
		publisherFactory: func() (*zmq4.Socket, error) {
			socket, err := zmq4.NewSocket(zmq4.PUB)
			if err != nil {
				return nil, err
			}
			if err := socket.Connect(eventEndpoint); err != nil {
				socket.Close()
				return nil, err
			}
			return socket, nil
		},
	}, nil
}

func (s *Service) JobNames() func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, name := range []string{"meme.refresh", "meme.dispatch"} {
			if !yield(name) {
				return
			}
		}
	}
}

func (s *Service) Run(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("scheduler service is nil")
	}

	s.logger.Printf(
		"config refresh_interval=%s dispatch_interval=%s bootstrap_grace=%s refresh_max_items=%d dispatch_limit=%d event_endpoint=%s topic=%s",
		s.refreshInterval,
		s.dispatchInterval,
		s.bootstrapGrace,
		s.refreshMaxItems,
		s.dispatchMemeLimit,
		s.eventEndpoint,
		s.topic,
	)

	if err := s.runMemeRefresh(ctx); err != nil {
		s.logger.Printf("initial meme refresh failed: %v", err)
	}
	if err := s.waitBootstrapGrace(ctx); err != nil {
		return err
	}
	if err := s.runMemeDispatch(ctx); err != nil {
		s.logger.Printf("bootstrap meme dispatch failed: %v", err)
	}

	refreshTicker := time.NewTicker(s.refreshInterval)
	defer refreshTicker.Stop()

	dispatchTicker := time.NewTicker(s.dispatchInterval)
	defer dispatchTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-refreshTicker.C:
			if err := s.runMemeRefresh(ctx); err != nil {
				s.logger.Printf("scheduled meme refresh failed: %v", err)
			}
		case <-dispatchTicker.C:
			if err := s.runMemeDispatch(ctx); err != nil {
				s.logger.Printf("scheduled meme dispatch failed: %v", err)
			}
		}
	}
}

func (s *Service) waitBootstrapGrace(ctx context.Context) error {
	if s.bootstrapGrace <= 0 {
		return nil
	}

	s.logger.Printf("bootstrap waiting grace_period=%s before first meme dispatch", s.bootstrapGrace)
	timer := time.NewTimer(s.bootstrapGrace)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) runMemeRefresh(ctx context.Context) error {
	event := newEvent("meme.pool.refresh", map[string]any{
		"max_items_per_scraper": s.refreshMaxItems,
	}, map[string]any{
		"interface": "scheduler",
		"schedule":  "meme.refresh",
	})

	s.logger.Printf("emit type=%s max_items_per_scraper=%d", event.Type, s.refreshMaxItems)
	return s.publish(ctx, event)
}

func (s *Service) runMemeDispatch(ctx context.Context) error {
	event := newEvent("meme.dispatch.scheduled", map[string]any{
		"limit": s.dispatchMemeLimit,
	}, map[string]any{
		"interface": "scheduler",
		"schedule":  "meme.dispatch",
	})

	s.logger.Printf("emit type=%s limit=%d", event.Type, s.dispatchMemeLimit)
	return s.publish(ctx, event)
}

func (s *Service) publish(ctx context.Context, event Event) error {
	socket, err := s.publisherFactory()
	if err != nil {
		return fmt.Errorf("create publisher: %w", err)
	}
	defer socket.Close()

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(defaultPublishSocketWarmup):
	}

	if _, err := socket.Send(s.topic, zmq4.SNDMORE); err != nil {
		return fmt.Errorf("send topic: %w", err)
	}
	if _, err := socket.Send(string(body), 0); err != nil {
		return fmt.Errorf("send event: %w", err)
	}
	return nil
}

func newEvent(eventType string, payload map[string]any, context map[string]any) Event {
	return Event{
		EventID:   uuid.NewString(),
		Timestamp: time.Now().UTC(),
		Source:    "scheduler",
		Type:      eventType,
		User: EventUser{
			ID:   "dokja-scheduler",
			Name: "dokja-scheduler",
		},
		Channel: EventChannel{
			ID: "scheduler",
		},
		Payload: payload,
		Context: context,
	}
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: parse duration: %w", key, err)
	}
	return value, nil
}

func intFromEnv(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: parse int: %w", key, err)
	}
	return value, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
