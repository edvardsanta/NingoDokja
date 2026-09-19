package main

import (
	"errors"
	"os"
	"os/signal"
	store "read_books/dokja_store"
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

	controls, err := core.NewControls(os.Getenv("VA_STATE_FILE"))
	if err != nil {
		logger.Error("load controls state", err)
		os.Exit(1)
	}
	if note := controls.LoadNote(); note != "" {
		logger.Error("controls state", errors.New(note))
	}

	// The shared database (chat provider profiles). Optional: without it the chat service
	// keeps using its environment settings.
	var profiles handlers.ChatProfiles
	if path := os.Getenv("DOKJA_DB_FILE"); path != "" {
		db, err := store.Open(path)
		if err != nil {
			logger.Error("open shared database; chat profiles are disabled", err)
		} else {
			defer db.Close()
			profiles = db
		}
	}

	memeClient := clients.NewMemeServiceClient("")

	service := core.DefaultService(
		handlers.NewSystemDomainHandler(
			clients.NewChatAIServiceClient(""),
			memeClient,
			memeClient,
			clients.NewDiscordInterfaceClient(""),
			os.Getenv("DISCORD_SCHEDULED_MEME_CHANNEL_ID"),
		).WithSafeOnlyChannels(os.Getenv("DISCORD_SAFE_ONLY_CHANNEL_IDS")).WithMemeTools(memeClient, memeClient).WithControls(controls).WithProfiles(profiles),
		handlers.NewModerationDomainHandler(nil),
		handlers.NewBookDomainHandler(clients.NewBookServiceClient("")),
		handlers.NewChatDomainHandler(clients.NewChatAIServiceClient("")),
		handlers.NewMemeDomainHandler(clients.NewMemeServiceClient("")),
		handlers.NewLoggingDomainHandler(core.DomainMemory),
		handlers.NewLoggingDomainHandler(core.DomainAutomation),
	).WithControls(controls)

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
