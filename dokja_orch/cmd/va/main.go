package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	digestdomain "read_books/dokja_domain/dokja_digest"
	store "read_books/dokja_store"
	"read_books/internal/app"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"read_books/internal/ingress"
	"read_books/internal/logger"
	"strconv"
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
	knowledgeClient := clients.NewKnowledgeServiceClient("")
	memoryClient := clients.NewMemoryServiceClient("")
	feedsClient := clients.NewFeedsServiceClient("")
	bookClient := clients.NewBookServiceClient("")
	memoryEnabled := func() bool { return controls == nil || controls.ServiceEnabled("memory") }
	hashtagExperience := handlers.NewHashtagExperience(memoryClient, memoryEnabled)
	hashtagMinScore := 0.6
	if configured, parseErr := strconv.ParseFloat(os.Getenv("MEME_HASHTAG_MIN_SCORE"), 64); parseErr == nil && configured >= 0 && configured <= 1 {
		hashtagMinScore = configured
	}
	hashtagWorkflow := handlers.NewMemeHashtagWorkflow(memeClient, knowledgeClient).
		WithMinScore(hashtagMinScore).
		WithKnowledgeEnabled(func() bool { return controls == nil || controls.ServiceEnabled("knowledge") })

	// Configure probes only when an endpoint exists. The handler still advertises
	// known interfaces without one, so missing setup never hides their desktop row.
	interfaceProbes := map[string]handlers.InterfaceProbe{}
	for name, variable := range map[string]string{
		"discord":  "DISCORD_INTERFACE_STATUS_ENDPOINT",
		"whatsapp": "WHATSAPP_INTERFACE_STATUS_ENDPOINT",
	} {
		if client := clients.NewInterfaceStatusClient(os.Getenv(variable)); client != nil {
			interfaceProbes[name] = client.Status
		}
	}

	// The interfaces that link to an account, linkable from the desktop through the communications
	// actions (and so behind their credential). Their address is the status address.
	interfaceLinks := map[string]handlers.InterfaceLinker{}
	if client := clients.NewInterfaceLinkClient(os.Getenv("WHATSAPP_INTERFACE_STATUS_ENDPOINT")); client != nil {
		interfaceLinks["whatsapp"] = client
	}

	// The conversations the owner named on the WhatsApp interface are listed and read next to the
	// Discord channels, and only when the interface's address is set.
	communicationsHandler := handlers.NewCommunicationsDomainHandler(
		clients.NewCommunicationsServiceClient(os.Getenv("COMMUNICATIONS_SERVICE_ENDPOINT"), os.Getenv("DOKJA_COMMUNICATIONS_TOKEN")),
		os.Getenv("DISCORD_SCHEDULED_MEME_CHANNEL_ID"),
	)
	// A nil client must stay a nil interface, so "not configured" is still seen as such.
	var whatsappSender handlers.WhatsAppSender
	if whatsappConversations := clients.NewWhatsAppConversationsClient(os.Getenv("WHATSAPP_INTERFACE_STATUS_ENDPOINT")); whatsappConversations != nil {
		communicationsHandler.WithConversations(whatsappConversations)
		whatsappSender = whatsappConversations
	}

	service := core.DefaultService(
		communicationsHandler,
		handlers.NewSystemDomainHandler(
			clients.NewChatAIServiceClient(""),
			memeClient,
			memeClient,
			clients.NewDiscordInterfaceClient(""),
			os.Getenv("DISCORD_SCHEDULED_MEME_CHANNEL_ID"),
		).WithSafeOnlyChannels(os.Getenv("DISCORD_SAFE_ONLY_CHANNEL_IDS")).WithMemeTools(memeClient, memeClient).WithHashtagSuggestions(hashtagWorkflow).WithMemeAttachments(memeClient).WithControls(controls).WithProfiles(profiles).WithHashtagExperience(hashtagExperience).
			// Step 06: suggestions run as a typed task with a frozen shadow prediction. Removing
			// this line returns to the earlier path above.
			WithHashtagTask(handlers.NewHashtagTask(hashtagWorkflow, memoryClient, memoryEnabled), hashtagExperience).
			WithServiceProbes(map[string]handlers.ServiceProbe{
				"book":      handlers.HealthProbe(bookClient),
				"knowledge": handlers.StatusProbe(knowledgeClient),
				"memory":    handlers.StatusProbe(memoryClient),
				"feeds":     handlers.FeedsProbe(feedsClient),
			}).WithInterfaceProbes(interfaceProbes).WithInterfaceLinks(interfaceLinks).WithWhatsApp(whatsappSender),
		handlers.NewModerationDomainHandler(nil),
		handlers.NewBookDomainHandler(bookClient),
		handlers.NewKnowledgeDomainHandler(knowledgeClient).WithReading(handlers.NewReadingTask(
			feedsClient, knowledgeClient, func() bool { return controls == nil || controls.ServiceEnabled("feeds") },
		)),
		handlers.NewDigestDomainHandler(feedsClient),
		handlers.NewChatDomainHandler(clients.NewChatAIServiceClient("")).WithKnowledge(
			knowledgeClient,
			func() bool { return controls == nil || controls.ServiceEnabled("knowledge") },
		).WithReading(func(ctx context.Context, id string) (digestdomain.Reference, error) {
			if controls != nil && !controls.ServiceEnabled("feeds") {
				return digestdomain.Reference{}, fmt.Errorf("reading sources are disabled")
			}
			return digestdomain.New(feedsClient).Reference(ctx, id)
		}),
		handlers.NewMemeDomainHandler(memeClient).WithHashtagWorkflow(hashtagWorkflow).WithHashtagExperience(hashtagExperience),
		handlers.NewMemoryDomainHandler(memoryClient),
		handlers.NewLoggingDomainHandler(core.DomainAutomation),
	).WithControls(controls).WithCommunicationsToken(os.Getenv("DOKJA_COMMUNICATIONS_TOKEN"))

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
