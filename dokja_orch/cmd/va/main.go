package main

import (
	"os"
	"os/signal"
	"read_books/internal/app"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"read_books/internal/ingress"
	"read_books/internal/logger"
	"syscall"
)

func main() {
	logger.Init()
	logger.Info("Starting Virtual Assistant (VA) orchestrator...")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	service := core.DefaultService(
		handlers.NewSystemDomainHandler(
			clients.NewChatAIServiceClient(""),
			clients.NewMemeServiceClient(""),
			clients.NewMemeServiceClient(""),
			clients.NewDiscordInterfaceClient(""),
			os.Getenv("DISCORD_SCHEDULED_MEME_CHANNEL_ID"),
		),
		handlers.NewModerationDomainHandler(nil),
		handlers.NewChatDomainHandler(clients.NewChatAIServiceClient("")),
		handlers.NewMemeDomainHandler(clients.NewMemeServiceClient("")),
		handlers.NewLoggingDomainHandler(core.DomainMemory),
		handlers.NewLoggingDomainHandler(core.DomainAutomation),
	)

	eventIngress := ingress.NewZeroMQEventIngress(service, os.Getenv("VA_ZMQ_ENDPOINT"), "")
	requestIngress := ingress.NewZeroMQRequestIngress(service, os.Getenv("VA_ZMQ_REQUEST_ENDPOINT"))
	httpBridgeIngress := ingress.NewHTTPBridgeIngress(service, os.Getenv("VA_HTTP_ENDPOINT"))

	application := app.NewApp(
		eventIngress,
		requestIngress,
		httpBridgeIngress,
	)

	if err := application.Run(signalChan(stop)); err != nil {
		logger.Error("Virtual Assistant (VA) orchestrator failed", err)
		return
	}

	logger.Info("Virtual Assistant (VA) orchestrator shut down.")
}

func signalChan(stop <-chan os.Signal) <-chan struct{} {
	out := make(chan struct{})

	go func() {
		<-stop
		close(out)
	}()

	return out
}
