package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"dokja_services/dokja_feeds/feeds"
	"dokja_services/dokja_feeds/server"
)

func main() {
	logger := log.New(os.Stdout, "[dokja-feeds] ", log.LstdFlags|log.LUTC)

	config := feeds.ConfigFromEnv(os.Getenv)
	service := feeds.New(config, feeds.Options{Logger: logger})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Printf("starting plugins_dir_set=%v", config.PluginsDir != "")
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx)
	}()

	err := server.Serve(ctx, config.Endpoint, service, logger)
	cancel()
	<-done
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatalf("feeds service stopped with error: %v", err)
	}
	logger.Printf("feeds service stopped")
}
