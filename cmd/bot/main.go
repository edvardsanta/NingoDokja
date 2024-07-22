package main

import (
	"os"
	"os/signal"
	"read_books/internal/bot"
	"read_books/internal/config"
	"read_books/internal/logger"
	"syscall"
)

func main() {
	logger.Init()
	config.LoadConfig(".env", "env", ".")

	bot := bot.NewBot(config.BotToken, config.NewsChannelID, config.GuildID)
	bot.AddHandlers()

	err := bot.Open()
	if err != nil {
		logger.Error("Erro ao abrir a conexão com o Discord", err)
		return
	}

	logger.Info("Bot está rodando. Pressione CTRL+C para sair.")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-stop

	bot.Close()
}
