package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"dokja_services/dokja_memory/memory"
	"dokja_services/dokja_memory/server"
)

func main() {
	logger := log.New(os.Stdout, "[dokja-memory] ", log.LstdFlags|log.LUTC)

	config, err := memory.ConfigFromEnv(os.Getenv)
	if err != nil {
		logger.Fatalf("configure memory service: %v", err)
	}
	service, closeStore, err := memory.Build(config, logger)
	if err != nil {
		logger.Fatalf("start memory service: %v", err)
	}
	defer closeStore()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Printf("starting embeddings=%v model=%s expire_after=%s", config.EmbedEnabled, config.EmbedModel, config.ExpireAfter)
	if err := server.Serve(ctx, config.Endpoint, service, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatalf("memory service stopped with error: %v", err)
	}
	logger.Printf("memory service stopped")
}
