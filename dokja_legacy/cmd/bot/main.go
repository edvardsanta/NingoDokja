package main

import (
	"os"
	"os/signal"
	"read_books/internal/legacy/bot"
	"read_books/internal/logger"
	"syscall"
)

func main() {
	logger.Init()
	logger.Info("[WARNING] This entry point (cmd/bot) will be migrated to 'cmd/va' soon. Please use 'cmd/va/main.go' to start the Virtual Assistant (VA), which now manages the bot as a component. For now, this entry point is still available for compatibility.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	go bot.StartBotComponent(stop)

	<-stop
	logger.Info("Discord bot shutting down.")
}
