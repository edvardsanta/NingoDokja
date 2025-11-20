package main

import (
	"os"
	"os/signal"
	"read_books/internal/bot"
	"read_books/internal/logger"
	"syscall"
)

func main() {
	logger.Init()
	logger.Info("Starting Virtual Assistant (VA)...")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	// Start the bot as a component
	go bot.StartBotComponent(stop)
	// Future: go startWeatherComponent(stop)
	// Future: go startAccessComponent(stop)

	<-stop
	logger.Info("Virtual Assistant (VA) shutting down.")
}
