package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	bookdomain "read_books/dokja_domain/dokja_book"
	communications "read_books/dokja_domain/dokja_communications"
	conversation "read_books/dokja_domain/dokja_conversation"
	digestdomain "read_books/dokja_domain/dokja_digest"
	memedomain "read_books/dokja_domain/dokja_meme"
	store "read_books/dokja_store"
	"read_books/internal/app"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/handlers"
	chatsessions "read_books/internal/infrastructure/chat_sessions"
	"read_books/internal/infrastructure/conversations"
	"read_books/internal/ingress"
	"read_books/internal/logger"
	"read_books/internal/ownerconfig"
	"strconv"
	"syscall"
	"time"
)

func main() {
	logger.Init()
	logger.Info("Starting Virtual Assistant (VA) orchestrator...")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	ownerConfig, err := ownerconfig.Load(os.Getenv("DOKJA_OWNER_CONFIG_FILE"))
	if err != nil {
		logger.Error("load owner configuration", err)
		os.Exit(1)
	}

	controls, err := core.NewControls(os.Getenv("VA_STATE_FILE"))
	if err != nil {
		logger.Error("load controls state", err)
		os.Exit(1)
	}
	if note := controls.LoadNote(); note != "" {
		logger.Error("controls state", errors.New(note))
	}
	sharedPath := os.Getenv("DOKJA_DB_FILE")
	if sharedPath == "" {
		sharedPath = filepath.Join(".dokja", "dokja.db")
	}
	chatSessions, err := chatsessions.Open(chatsessions.PathBeside(sharedPath))
	if err != nil {
		logger.Error("open durable chat sessions", err)
		os.Exit(1)
	}
	defer chatSessions.Close()

	// Archived profiles remain readable for compatibility; live chat selection
	// belongs to the host runtime provider configuration.
	var profiles handlers.ChatProfiles
	// The owner's conversations live in a file of their own beside the shared database, opened by
	// the orchestrator alone. Without it, conversations are not kept and chat goes on as before.
	var archive *conversation.Archive
	if path := os.Getenv("DOKJA_DB_FILE"); path != "" {
		db, err := store.Open(path)
		if err != nil {
			logger.Error("open shared database; chat profiles are disabled", err)
		} else {
			defer db.Close()
			profiles = db
		}
		kept, err := conversations.Open(conversations.PathBeside(path))
		if err != nil {
			logger.Error("open conversation archive; conversations are not kept", err)
		} else {
			defer kept.Close()
			archive = conversation.New(kept)
			// Turns a stopped orchestrator left pending are marked interrupted; none is sent again.
			if n, err := archive.Recover(context.Background(), time.Now().UTC()); err != nil {
				logger.Error("recover pending conversation turns", err)
			} else if n > 0 {
				logger.Info(fmt.Sprintf("conversation archive marked %d pending turns interrupted", n))
			}
		}
	}

	knowledgeClient := clients.NewKnowledgeServiceClient("")
	memoryClient := clients.NewMemoryServiceClient("")
	feedsClient := clients.NewFeedsServiceClient("")
	backend := os.Getenv("DOKJA_CONTENT_BACKEND")
	if backend != "" && backend != "feeds" && backend != "legacy" {
		logger.Error("invalid DOKJA_CONTENT_BACKEND", fmt.Errorf("expected feeds or legacy"))
		os.Exit(1)
	}
	contentPlugins := backend != "legacy"
	var memeClient interface {
		memedomain.Service
		ReadText(context.Context, string) (map[string]any, error)
		MarkSent(context.Context, string) error
		FetchAttachment(context.Context, string) ([]byte, string, error)
	} = memedomain.NewReadingService(feedsClient)
	var bookClient bookdomain.Service = bookdomain.NewReadingService(feedsClient)
	serviceProbes := map[string]handlers.ServiceProbe{
		"knowledge": handlers.StatusProbe(knowledgeClient), "memory": handlers.StatusProbe(memoryClient), "feeds": handlers.FeedsProbe(feedsClient),
	}
	if contentPlugins {
		controls.UseContentPlugins()
	} else {
		memeClient = clients.NewMemeServiceClient("")
		legacyBook := clients.NewBookServiceClient("")
		bookClient = legacyBook
		serviceProbes["book"] = handlers.HealthProbe(legacyBook)
	}
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
	inboundPolicy, err := communications.ParseInboundPolicy(map[string]string{
		"DISCORD_GUILD_ID":              os.Getenv("DISCORD_GUILD_ID"),
		"DISCORD_ALLOWED_CHANNELS":      os.Getenv("DISCORD_ALLOWED_CHANNELS"),
		"DISCORD_DM_POLICY":             os.Getenv("DISCORD_DM_POLICY"),
		"DISCORD_REQUIRE_MENTION":       os.Getenv("DISCORD_REQUIRE_MENTION"),
		"DOKJA_WHATSAPP_SELF_CHAT":      os.Getenv("DOKJA_WHATSAPP_SELF_CHAT"),
		"DOKJA_WHATSAPP_ALLOW_FROM":     os.Getenv("DOKJA_WHATSAPP_ALLOW_FROM"),
		"DOKJA_WHATSAPP_ALLOWED_GROUPS": os.Getenv("DOKJA_WHATSAPP_ALLOWED_GROUPS"),
	})
	if err != nil {
		logger.Error("inbound communications policy", err)
		os.Exit(1)
	}
	voiceServiceEndpoint := os.Getenv("DOKJA_VOICE_HTTP_ENDPOINT")
	voiceGenerator, err := voiceGeneratorForProvider(os.Getenv("DOKJA_VOICE_CHAT_PROVIDER"), voiceServiceEndpoint)
	if err != nil {
		logger.Error("configure voice chat provider", err)
		os.Exit(1)
	}
	logger.Info(fmt.Sprintf("voice chat provider configured local=%t", voiceGenerator != nil))
	voiceServiceClient := clients.NewVoiceServiceClient(voiceServiceEndpoint)
	communicationsHandler := handlers.NewCommunicationsDomainHandler(
		clients.NewCommunicationsServiceClient(os.Getenv("COMMUNICATIONS_SERVICE_ENDPOINT"), os.Getenv("DOKJA_COMMUNICATIONS_TOKEN")),
		os.Getenv("DISCORD_SCHEDULED_MEME_CHANNEL_ID"),
	).WithInboundPolicy(inboundPolicy).WithGuilds(os.Getenv("DISCORD_GUILD_ID")).
		// Voice channel queues find their tracks through the music sources installed as Feeds plugins.
		WithMusic(clients.NewFeedsAudioSources(feedsClient, communications.AudioCapability)).
		WithVoice(voiceServiceClient)
	// A nil client must stay a nil interface, so "not configured" is still seen as such.
	var whatsappSender handlers.WhatsAppSender
	if whatsappConversations := clients.NewWhatsAppConversationsClient(os.Getenv("WHATSAPP_INTERFACE_STATUS_ENDPOINT")); whatsappConversations != nil {
		communicationsHandler.WithConversations(whatsappConversations)
		whatsappSender = whatsappConversations
	}

	service := core.DefaultService(
		communicationsHandler,
		handlers.NewSystemDomainHandler(
			clients.NewChatProviderClient(""),
			memeClient,
			memeClient,
			clients.NewDiscordInterfaceClient(""),
			os.Getenv("DISCORD_SCHEDULED_MEME_CHANNEL_ID"),
		).WithSafeOnlyChannels(os.Getenv("DISCORD_SAFE_ONLY_CHANNEL_IDS")).WithMemeTools(memeClient, memeClient).WithHashtagSuggestions(hashtagWorkflow).WithMemeAttachments(memeClient).WithControls(controls).WithProfiles(profiles).WithHashtagExperience(hashtagExperience).
			// Step 06: suggestions run as a typed task with a frozen shadow prediction. Removing
			// this line returns to the earlier path above.
			WithHashtagTask(handlers.NewHashtagTask(hashtagWorkflow, memoryClient, memoryEnabled), hashtagExperience).
			WithContentPlugins(contentPlugins).WithServiceProbes(serviceProbes).WithInterfaceProbes(interfaceProbes).WithInterfaceLinks(interfaceLinks).WithWhatsApp(whatsappSender).
			// The operator may write to any channel the Communications domain lets them read.
			WithManualTargets(communicationsHandler.Readable),
		handlers.NewModerationDomainHandler(nil),
		handlers.NewBookDomainHandler(bookClient),
		handlers.NewKnowledgeDomainHandler(knowledgeClient).WithApplicationProposer(
			clients.NewKnowledgeProposalClient(clients.NewChatProviderClient("")),
		).WithMeasuredApplicationProposer(
			clients.NewMeasuredKnowledgeProposalClient(clients.NewChatProviderClient("")),
		).WithReading(handlers.NewReadingTask(
			feedsClient, knowledgeClient, func() bool { return controls == nil || controls.ServiceEnabled("feeds") },
		)),
		handlers.NewDigestDomainHandler(feedsClient),
		handlers.NewChatDomainHandler(clients.NewChatProviderClient("")).WithOwnerProfile(ownerConfig.Owner.Profile).WithVoiceGenerator(voiceGenerator).WithSessionStore(chatSessions).WithKnowledge(
			knowledgeClient,
			func() bool { return controls == nil || controls.ServiceEnabled("knowledge") },
		).WithReading(func(ctx context.Context, id string) (digestdomain.Reference, error) {
			if controls != nil && !controls.ServiceEnabled("feeds") {
				return digestdomain.Reference{}, fmt.Errorf("reading sources are disabled")
			}
			return digestdomain.New(feedsClient).Reference(ctx, id)
		}).WithRecentReadings(func(ctx context.Context, question string, limit int) ([]digestdomain.Reference, error) {
			if controls != nil && !controls.ServiceEnabled("feeds") {
				return nil, fmt.Errorf("reading sources are disabled")
			}
			return digestdomain.New(feedsClient).RecentReferences(ctx, question, limit)
		}).WithArchive(archive),
		handlers.NewMemeDomainHandler(memeClient).WithHashtagWorkflow(hashtagWorkflow).WithHashtagExperience(hashtagExperience),
		handlers.NewMemoryDomainHandler(memoryClient),
		handlers.NewConversationDomainHandler(archive).WithRealtime(conversation.NewConversationContextEngine()),
		handlers.NewLoggingDomainHandler(core.DomainAutomation),
	).WithControls(controls).WithCommunicationsToken(os.Getenv("DOKJA_COMMUNICATIONS_TOKEN"))

	eventIngress := ingress.NewZeroMQEventIngress(service, os.Getenv("VA_ZMQ_ENDPOINT"), "")
	requestIngress := ingress.NewZeroMQRequestIngress(service, os.Getenv("VA_ZMQ_REQUEST_ENDPOINT"))
	httpBridgeIngress := ingress.NewHTTPBridgeIngress(service, os.Getenv("VA_HTTP_ENDPOINT")).WithOwnerConfig(ownerConfig)

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
