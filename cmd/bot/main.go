package main

import (
	"os"
	"os/signal"
	"read_books/internal/bot"
	"read_books/internal/config"
	"read_books/internal/logger"
	"syscall"
	"time"

	"github.com/avast/retry-go"
)

func main() {
	logger.Init()
	logger.Info("Coletando arquivos de configuração...")
	config.LoadConfig()
	// db.InitDB(config.RedisAddr)
	logger.Info("Configurações coletadas")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	go runBot(stop)

	<-stop
	logger.Info("Ningo Dokja está encerrando.")
}

func runBot(stop chan os.Signal) {
	err := retry.Do(
		func() error {
			b := bot.NewBot(
				config.AppConfig.Bot.Token,
				config.AppConfig.Bot.NewsChannelID,
				config.AppConfig.Bot.GuildID,
				config.AppConfig.Bot.OlympicChannelID,
				config.AppConfig.Bot.OlympicChannelFinishedID,
				config.AppConfig.Bot.OlympicChannelRunningID,
			)
			b.AddHandlers()

			err := b.Open()
			if err != nil {
				logger.Error("Erro ao abrir a conexão com o Discord", err)
				return err
			}

			logger.Info("Ningo Dokja está rodando. Pressione CTRL+C para sair.")
			<-stop
			b.Close()
			return nil
		},
		retry.Attempts(3),                 // Tenta 3 vezes
		retry.Delay(5*time.Second),        // Espera 5 segundos entre as tentativas
		retry.DelayType(retry.FixedDelay), // Usa um delay fixo
	)

	if err != nil {
		logger.Error("Erro ao executar o bot", err)
	}
}
