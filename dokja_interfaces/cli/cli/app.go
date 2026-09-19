package cli

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type App struct {
	newPublisher func(endpoint string) (*Publisher, error)
	newRequester func(endpoint string) (*Requester, error)
	emitOpts     emitCommandOptions
}

type emitCommandOptions struct {
	eventEndpoint   string
	requestEndpoint string
	topic           string
	userID          string
	userName        string
	channelID       string
	payload         map[string]string
	timeout         time.Duration
}

const defaultRequestTimeout = 2 * time.Minute

func NewApp() *App {
	return &App{
		newPublisher: NewPublisher,
		newRequester: NewRequester,
		emitOpts: emitCommandOptions{
			eventEndpoint:   envOrDefault("DOKJA_ORCH_ENDPOINT", DefaultEndpoint),
			requestEndpoint: envOrDefault("DOKJA_ORCH_REQUEST_ENDPOINT", DefaultRequestEndpoint),
			topic:           envOrDefault("DOKJA_ORCH_TOPIC", DefaultTopic),
			userID:          "cli-user",
			userName:        currentUserName(),
			channelID:       "cli",
			payload:         map[string]string{},
			timeout:         defaultRequestTimeout,
		},
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	cmd := a.newRootCommand(ctx)
	cmd.SetArgs(args[1:])
	return cmd.Execute()
}

func (a *App) newRootCommand(ctx context.Context) *cobra.Command {
	root := &cobra.Command{
		Use:   "dokja-cli",
		Short: "Dokja CLI interface",
		Long:  "CLI interface that emits ZeroMQ events to the orchestrator.",
	}

	root.PersistentFlags().StringVar(&a.emitOpts.eventEndpoint, "endpoint", a.emitOpts.eventEndpoint, "Orchestrator ZeroMQ event endpoint")
	root.PersistentFlags().StringVar(&a.emitOpts.requestEndpoint, "request-endpoint", a.emitOpts.requestEndpoint, "Orchestrator ZeroMQ request endpoint")
	root.PersistentFlags().StringVar(&a.emitOpts.topic, "topic", a.emitOpts.topic, "Orchestrator ZeroMQ topic")
	root.PersistentFlags().StringVar(&a.emitOpts.userID, "user-id", a.emitOpts.userID, "Event user id")
	root.PersistentFlags().StringVar(&a.emitOpts.userName, "user-name", a.emitOpts.userName, "Event user name")
	root.PersistentFlags().StringVar(&a.emitOpts.channelID, "channel-id", a.emitOpts.channelID, "Event channel id")
	root.PersistentFlags().DurationVar(&a.emitOpts.timeout, "timeout", a.emitOpts.timeout, "How long to wait for the orchestrator to answer")

	root.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show service health and delivery channels (ningo.status)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.request(ctx, EmitOptions{
				Type:      "ningo.status",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   map[string]any{},
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	})
	root.AddCommand(a.newMemeCommand(ctx))
	root.AddCommand(a.newBookCommand(ctx))
	root.AddCommand(a.newDiscordCommand(ctx))
	root.AddCommand(a.newServicesCommand(ctx))
	root.AddCommand(a.newJobsCommand(ctx))
	root.AddCommand(a.newChatCommand(ctx))
	root.AddCommand(a.newEmitCommand(ctx))

	return root
}

func (a *App) newMemeCommand(ctx context.Context) *cobra.Command {
	memeCommand := &cobra.Command{
		Use:   "meme",
		Short: "Request meme-related actions from the orchestrator",
	}

	var fetchLimit int
	fetch := &cobra.Command{
		Use:   "fetch",
		Short: "Emit meme.fetch",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			if fetchLimit > 0 {
				payload["limit"] = fetchLimit
			}
			return a.request(ctx, EmitOptions{
				Type:      "meme.fetch",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	fetch.Flags().IntVar(&fetchLimit, "limit", 0, "Number of memes requested")

	var maxItems int
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Emit meme.pool.refresh",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			if maxItems > 0 {
				payload["max_items_per_scraper"] = maxItems
			}
			return a.request(ctx, EmitOptions{
				Type:      "meme.pool.refresh",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	refresh.Flags().IntVar(&maxItems, "max-items", 0, "Max items per scraper")

	status := &cobra.Command{
		Use:     "status",
		Aliases: []string{"pool"},
		Short:   "Show the meme pool: unsent and sent counts (meme.status)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.request(ctx, EmitOptions{
				Type:      "meme.status",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   map[string]any{},
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}

	var dispatchLimit int
	dispatch := &cobra.Command{
		Use:   "dispatch",
		Short: "Send memes now to every scheduled meme channel (meme.dispatch.scheduled)",
		Long: "Runs the same delivery the scheduler runs every few hours, right now, and prints " +
			"how many memes were delivered. Channels listed in DISCORD_SAFE_ONLY_CHANNEL_IDS skip " +
			"memes the NSFW screen flagged.",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildDispatchPayload(dispatchLimit)
			if err != nil {
				return err
			}
			return a.request(ctx, EmitOptions{
				Type:      "meme.dispatch.scheduled",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	dispatch.Flags().IntVar(&dispatchLimit, "limit", 1, "How many memes to send (at least 1)")

	screen := &cobra.Command{
		Use:   "screen <image-url>",
		Short: "Run an image through the NSFW filter and show what the model detected (meme.screen)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.request(ctx, EmitOptions{
				Type:      "meme.screen",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   map[string]any{"url": strings.TrimSpace(args[0])},
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}

	var listScope string
	var listLimit, listOffset int
	list := &cobra.Command{
		Use:   "list",
		Short: "Browse the pool without consuming it (meme.list)",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildListPayload(listScope, listLimit, listOffset)
			if err != nil {
				return err
			}
			return a.request(ctx, EmitOptions{
				Type:      "meme.list",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	list.Flags().StringVar(&listScope, "scope", "unsent", "Which memes to list: unsent or sent")
	list.Flags().IntVar(&listLimit, "limit", 20, "Page size (1-100)")
	list.Flags().IntVar(&listOffset, "offset", 0, "Rows to skip")

	memeCommand.AddCommand(fetch, refresh, status, dispatch, screen, list)
	return memeCommand
}

func buildListPayload(scope string, limit, offset int) (map[string]any, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope != "unsent" && scope != "sent" {
		return nil, fmt.Errorf("--scope must be unsent or sent, got %q", scope)
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("--limit must be between 1 and 100, got %d", limit)
	}
	if offset < 0 {
		return nil, fmt.Errorf("--offset must not be negative, got %d", offset)
	}
	return map[string]any{"scope": scope, "limit": limit, "offset": offset}, nil
}

func buildDispatchPayload(limit int) (map[string]any, error) {
	// A missing limit means "everything in the pool" to the orchestrator, so never omit it.
	if limit < 1 {
		return nil, fmt.Errorf("--limit must be at least 1, got %d", limit)
	}
	return map[string]any{"limit": limit}, nil
}

func (a *App) newDiscordCommand(ctx context.Context) *cobra.Command {
	discordCommand := &cobra.Command{
		Use:   "discord",
		Short: "Administrative Discord actions through the orchestrator",
	}

	var channels []string
	var all bool
	var text string
	var image string
	var markSent bool
	send := &cobra.Command{
		Use:   "send",
		Short: "Send a message to configured Discord channels (discord.send)",
		Long: "Sends text and/or an image to the chosen channels. Which channel you pick decides " +
			"how it is sent: a channel with a webhook configured goes through the webhook, any " +
			"other goes through the bot. Only channels the orchestrator already delivers to are accepted. " +
			"Images bound for a safe-only channel are screened first and skipped if flagged.",
		Example: "  dokja-cli discord send --all --text \"teste kkk\"\n" +
			"  dokja-cli discord send --channel 123456789012345678 --text oi --image https://example.com/a.png",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildDiscordSendPayload(channels, all, text, image)
			if err != nil {
				return err
			}
			if markSent {
				if image == "" {
					return fmt.Errorf("--mark-sent only applies together with --image")
				}
				payload["mark_sent"] = true
			}
			return a.request(ctx, EmitOptions{
				Type:      "discord.send",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	send.Flags().StringSliceVar(&channels, "channel", nil, "Channel id to send to (repeatable or comma-separated)")
	send.Flags().BoolVar(&all, "all", false, "Send to every scheduled meme channel (bot channel and webhook)")
	send.Flags().StringVar(&text, "text", "", "Message text")
	send.Flags().StringVar(&image, "image", "", "Image URL to attach")
	send.Flags().BoolVar(&markSent, "mark-sent", false, "Mark the attached meme as sent so the scheduler does not repeat it")

	discordCommand.AddCommand(send)
	return discordCommand
}

func buildDiscordSendPayload(channels []string, all bool, text, image string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	image = strings.TrimSpace(image)
	if text == "" && image == "" {
		return nil, fmt.Errorf("provide --text and/or --image")
	}

	cleaned := make([]string, 0, len(channels))
	for _, channel := range channels {
		if channel = strings.TrimSpace(channel); channel != "" {
			cleaned = append(cleaned, channel)
		}
	}
	switch {
	case all && len(cleaned) > 0:
		return nil, fmt.Errorf("use either --all or --channel, not both")
	case !all && len(cleaned) == 0:
		return nil, fmt.Errorf("choose where to send: --channel <id> or --all")
	}

	payload := map[string]any{}
	if text != "" {
		payload["content"] = text
	}
	if image != "" {
		payload["attachment_url"] = image
	}
	if all {
		payload["all"] = true
	} else {
		payload["channel_ids"] = cleaned
	}
	return payload, nil
}

func (a *App) newBookCommand(ctx context.Context) *cobra.Command {
	bookCommand := &cobra.Command{
		Use:   "book",
		Short: "Request book classification and summary actions from the orchestrator",
	}

	var classifyTitle string
	var classifyGoal string
	var classifyLanguage string
	classify := &cobra.Command{
		Use:   "classify <file>",
		Short: "Emit book.resource.classify with an uploaded file payload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildBookFilePayload(args[0], classifyTitle, classifyGoal, classifyLanguage, false)
			if err != nil {
				return err
			}
			return a.request(ctx, EmitOptions{
				Type:      "book.resource.classify",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	classify.Flags().StringVar(&classifyTitle, "title", "", "Book title override")
	classify.Flags().StringVar(&classifyGoal, "goal", "study", "Reading goal")
	classify.Flags().StringVar(&classifyLanguage, "language", "pt-BR", "Content language hint")

	var summarizeTitle string
	var summarizeGoal string
	var summarizeLanguage string
	summarize := &cobra.Command{
		Use:   "summarize <file>",
		Short: "Emit book.summary.requested with an uploaded file payload",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildBookFilePayload(args[0], summarizeTitle, summarizeGoal, summarizeLanguage, true)
			if err != nil {
				return err
			}
			return a.request(ctx, EmitOptions{
				Type:      "book.summary.requested",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	summarize.Flags().StringVar(&summarizeTitle, "title", "", "Book title override")
	summarize.Flags().StringVar(&summarizeGoal, "goal", "study", "Reading goal")
	summarize.Flags().StringVar(&summarizeLanguage, "language", "pt-BR", "Content language hint")

	bookCommand.AddCommand(classify, summarize)
	return bookCommand
}

func (a *App) newChatCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "chat [message]",
		Short: "Send chat messages through the orchestrator",
		Long:  "If no message is provided, starts an interactive REPL session.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return a.chatOnce(ctx, strings.Join(args, " "), chatSessionOptions{
					sessionKey:      fmt.Sprintf("cli:direct:%s:%d", a.emitOpts.userID, time.Now().UTC().UnixNano()),
					forceNewSession: true,
				})
			}
			return a.chatREPL(ctx)
		},
	}

	command.AddCommand(a.newChatProfileCommand(ctx))
	return command
}

func (a *App) newEmitCommand(ctx context.Context) *cobra.Command {
	a.emitOpts.payload = map[string]string{}

	command := &cobra.Command{
		Use:   "emit <event-type>",
		Short: "Emit a generic asynchronous event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := make(map[string]any, len(a.emitOpts.payload))
			for key, value := range a.emitOpts.payload {
				payload[key] = normalizePayloadValue(value)
			}

			return a.emit(ctx, EmitOptions{
				Type:      args[0],
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}

	command.Flags().StringToStringVar(&a.emitOpts.payload, "payload", map[string]string{}, "Payload entries in key=value form")
	return command
}

func (a *App) emit(ctx context.Context, opts EmitOptions) error {
	publisher, err := a.newPublisher(a.emitOpts.eventEndpoint)
	if err != nil {
		return fmt.Errorf("create publisher: %w", err)
	}
	defer publisher.Close()

	event := BuildEvent(opts)
	if err := publisher.PublishEvent(ctx, a.emitOpts.topic, event); err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	fmt.Printf("emitted %s to %s topic %s as %s\n", event.Type, a.emitOpts.eventEndpoint, a.emitOpts.topic, event.EventID)
	return nil
}

func (a *App) call(ctx context.Context, opts EmitOptions) (RequestResponse, error) {
	if a.emitOpts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.emitOpts.timeout)
		defer cancel()
	}

	requester, err := a.newRequester(a.emitOpts.requestEndpoint)
	if err != nil {
		return RequestResponse{}, fmt.Errorf("create requester: %w", err)
	}
	defer requester.Close()

	response, err := requester.Request(ctx, BuildEvent(opts))
	if err != nil {
		return RequestResponse{}, fmt.Errorf("request event: %w", err)
	}
	return response, nil
}

func printJSON(value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("format response: %w", err)
	}
	fmt.Println(string(body))
	return nil
}

func (a *App) request(ctx context.Context, opts EmitOptions) error {
	response, err := a.call(ctx, opts)
	if err != nil {
		return err
	}
	return printJSON(response.Result)
}

type chatSessionOptions struct {
	sessionKey      string
	forceNewSession bool
}

func (a *App) chatOnce(ctx context.Context, message string, session chatSessionOptions) error {
	requester, err := a.newRequester(a.emitOpts.requestEndpoint)
	if err != nil {
		return fmt.Errorf("create requester: %w", err)
	}
	defer requester.Close()

	response, err := requester.Request(ctx, BuildEvent(EmitOptions{
		Type:      "message.created",
		UserID:    a.emitOpts.userID,
		UserName:  a.emitOpts.userName,
		ChannelID: a.emitOpts.channelID,
		Payload: map[string]any{
			"content": strings.TrimSpace(message),
		},
		Context: map[string]any{
			"interface":         "cli",
			"session_key":       strings.TrimSpace(session.sessionKey),
			"force_new_session": session.forceNewSession,
		},
		Source: "cli",
	}))
	if err != nil {
		return fmt.Errorf("request chat event: %w", err)
	}

	reply, err := extractChatReply(response.Result)
	if err != nil {
		return err
	}

	fmt.Println(reply)
	return nil
}

func (a *App) chatREPL(ctx context.Context) error {
	fmt.Println("dokja chat repl")
	fmt.Println("type 'exit' or 'quit' to stop")

	sessionKey := fmt.Sprintf("cli:repl:%s:%d", a.emitOpts.userID, time.Now().UTC().UnixNano())
	forceNewSession := true
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil
		}

		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case "exit", "quit":
			return nil
		}

		if err := a.chatOnce(ctx, line, chatSessionOptions{
			sessionKey:      sessionKey,
			forceNewSession: forceNewSession,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "chat error: %v\n", err)
		} else {
			forceNewSession = false
		}
	}
}

func extractChatReply(result any) (string, error) {
	processResult, ok := result.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected orchestrator response shape")
	}

	domainResult, ok := processResult["result"]
	if !ok {
		return "", fmt.Errorf("orchestrator response is missing result")
	}

	switch typed := domainResult.(type) {
	case map[string]any:
		if chatResult, ok := typed["chat"].(map[string]any); ok {
			reply, _ := chatResult["reply"].(string)
			if reply == "" {
				return "", fmt.Errorf("chat response is empty")
			}
			return reply, nil
		}

		reply, _ := typed["reply"].(string)
		if reply == "" {
			return "", fmt.Errorf("chat response is empty")
		}
		return reply, nil
	default:
		return "", fmt.Errorf("orchestrator response is missing chat result")
	}
}

func normalizePayloadValue(value string) any {
	if intValue, err := strconv.Atoi(value); err == nil {
		return intValue
	}
	return value
}

func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func currentUserName() string {
	if value := os.Getenv("USER"); value != "" {
		return value
	}
	return "cli"
}

func buildBookFilePayload(path, title, goal, language string, includeBytes bool) (map[string]any, error) {
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read book file: %w", err)
	}

	base := filepath.Base(path)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(base)), ".")
	if ext == "" {
		return nil, fmt.Errorf("book file extension is required to infer format")
	}

	payload := map[string]any{
		"filename":    base,
		"format":      ext,
		"goal":        strings.TrimSpace(goal),
		"language":    strings.TrimSpace(language),
		"metadata":    map[string]any{"uploaded_via": "cli"},
		"source_path": path,
	}
	if strings.TrimSpace(title) != "" {
		payload["title"] = strings.TrimSpace(title)
	}
	if includeBytes || ext == "pdf" || ext == "epub" || ext == "mobi" || ext == "md" || ext == "txt" {
		payload["resource_bytes_b64"] = base64.StdEncoding.EncodeToString(fileBytes)
	}
	if ext == "txt" || ext == "md" {
		payload["content"] = string(fileBytes)
	}
	return payload, nil
}
