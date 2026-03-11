package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"dokja_services/dokja_scheduler/scheduler"
)

func main() {
	logger := log.New(os.Stdout, "[dokja-scheduler] ", log.LstdFlags|log.LUTC)

	service, err := scheduler.NewFromEnv(logger)
	if err != nil {
		logger.Fatalf("configure scheduler: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Printf("starting jobs=%v", slices.Collect(service.JobNames()))
	if err := service.Run(ctx); err != nil && err != context.Canceled {
		logger.Fatalf("scheduler stopped with error: %v", err)
	}
	logger.Printf("scheduler stopped")
}
