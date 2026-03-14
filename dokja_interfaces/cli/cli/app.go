package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
}

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

	root.AddCommand(a.newMemeCommand(ctx))
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
		Use:   "status",
		Short: "Emit meme.status",
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

	memeCommand.AddCommand(fetch, refresh, status)
	return memeCommand
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

func (a *App) request(ctx context.Context, opts EmitOptions) error {
	requester, err := a.newRequester(a.emitOpts.requestEndpoint)
	if err != nil {
		return fmt.Errorf("create requester: %w", err)
	}
	defer requester.Close()

	event := BuildEvent(opts)
	response, err := requester.Request(ctx, event)
	if err != nil {
		return fmt.Errorf("request event: %w", err)
	}

	body, err := json.MarshalIndent(response.Result, "", "  ")
	if err != nil {
		return fmt.Errorf("format response: %w", err)
	}

	fmt.Println(string(body))
	return nil
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
