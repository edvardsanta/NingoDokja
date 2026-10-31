package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// The CLI keeps no conversations of its own. Everything here reads and writes the orchestrator's
// archive, the one the desktop and the phone use, so a conversation has the same id and the same
// turns wherever it is opened.

const (
	archiveIDLength   = 32
	shortIDLength     = 8
	conversationPage  = 50
	listPagesSearched = 5
	titleFromRunes    = 60
	defaultListed     = 20
	defaultTurnsRead  = 50
)

type archivedConversation struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
	Revision  int64  `json:"revision"`
	Turns     int    `json:"turns"`
}

// archivedTurn reads version 1 of the turn projection, pinned by the orchestrator's
// testdata/turn_view_v1.json. Provider and model are what answered, not what was selected.
type archivedTurn struct {
	View     int    `json:"view"`
	ID       string `json:"id"`
	Seq      int    `json:"seq"`
	SendID   string `json:"send_id"`
	Content  string `json:"content"`
	Reply    string `json:"reply"`
	State    string `json:"state"`
	Reason   string `json:"reason"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type conversationListing struct {
	Conversations []archivedConversation `json:"conversations"`
	Next          string                 `json:"next"`
}

type conversationReading struct {
	Conversation archivedConversation `json:"conversation"`
	Turns        []archivedTurn       `json:"turns"`
}

// archiveRequest asks the conversation domain one thing and gives its answer.
type archiveRequest func(eventType string, payload map[string]any) (map[string]any, error)

func (a *App) archiveRequester(ctx context.Context) archiveRequest {
	return func(eventType string, payload map[string]any) (map[string]any, error) {
		response, err := a.call(ctx, EmitOptions{
			Type:      eventType,
			UserID:    a.emitOpts.userID,
			UserName:  a.emitOpts.userName,
			ChannelID: a.emitOpts.channelID,
			Payload:   payload,
			Context:   map[string]any{"interface": "cli"},
			Source:    "cli",
		})
		if err != nil {
			return nil, err
		}
		if response.Status != "ok" {
			return nil, fmt.Errorf("%s", firstNonEmpty(response.Error, "orchestrator returned "+response.Status))
		}
		return conversationAnswer(response.Result)
	}
}

// conversationAnswer finds the domain's answer. A reply from one domain is flat and names the domain;
// a verbose one nests it under the name. This domain's own answers hold a "conversation" field, so
// the field alone cannot tell the two apart, and it would hide the turns beside it.
func conversationAnswer(result any) (map[string]any, error) {
	envelope, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected orchestrator response shape")
	}
	body, _ := envelope["result"].(map[string]any)
	if body == nil {
		return nil, fmt.Errorf("orchestrator response is missing result")
	}
	if domain, _ := envelope["domain"].(string); domain == "conversation" {
		return body, nil
	}
	if nested, ok := body["conversation"].(map[string]any); ok {
		return nested, nil
	}
	return nil, fmt.Errorf("orchestrator response is missing the conversation result")
}

func decodeAnswer[T any](answer map[string]any) (T, error) {
	var decoded T
	raw, err := json.Marshal(answer)
	if err != nil {
		return decoded, err
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return decoded, fmt.Errorf("unexpected conversation answer: %w", err)
	}
	return decoded, nil
}

func listConversations(ask archiveRequest, cursor string, limit int) (conversationListing, error) {
	payload := map[string]any{"limit": limit}
	if cursor != "" {
		payload["cursor"] = cursor
	}
	answer, err := ask("conversation.list", payload)
	if err != nil {
		return conversationListing{}, err
	}
	return decodeAnswer[conversationListing](answer)
}

func readConversation(ask archiveRequest, id string, limit int) (conversationReading, error) {
	answer, err := ask("conversation.read", map[string]any{"conversation_id": id, "limit": limit})
	if err != nil {
		return conversationReading{}, err
	}
	return decodeAnswer[conversationReading](answer)
}

// matchConversation accepts the short id the list prints as well as a whole one.
func matchConversation(ask archiveRequest, reference string) (string, error) {
	reference = strings.ToLower(strings.TrimSpace(reference))
	if reference == "" {
		return "", fmt.Errorf("a conversation id is required")
	}
	if len(reference) == archiveIDLength {
		return reference, nil
	}
	found := []string{}
	cursor := ""
	for range listPagesSearched {
		page, err := listConversations(ask, cursor, conversationPage)
		if err != nil {
			return "", err
		}
		for _, item := range page.Conversations {
			if strings.HasPrefix(item.ID, reference) {
				found = append(found, item.ID)
			}
		}
		if cursor = page.Next; cursor == "" {
			break
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no conversation starts with %q", reference)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("%d conversations start with %q; give more of the id", len(found), reference)
	}
}

// latestGeneral finds the conversation to continue: the most recent general one, as on the phone. A
// reading's discussion is continued only by naming it.
func latestGeneral(ask archiveRequest) (string, error) {
	cursor := ""
	for range listPagesSearched {
		page, err := listConversations(ask, cursor, conversationPage)
		if err != nil {
			return "", err
		}
		for _, item := range page.Conversations {
			if item.Kind == "general" {
				return item.ID, nil
			}
		}
		if cursor = page.Next; cursor == "" {
			break
		}
	}
	return "", nil
}

func startConversation(ask archiveRequest, title string) (string, error) {
	answer, err := ask("conversation.start", map[string]any{"title": title})
	if err != nil {
		return "", err
	}
	started, err := decodeAnswer[conversationReading](answer)
	if err != nil {
		return "", err
	}
	if started.Conversation.ID == "" {
		return "", fmt.Errorf("the archive named no conversation")
	}
	return started.Conversation.ID, nil
}

// ConversationTitle names a new conversation by the start of its first message.
func ConversationTitle(message string) string {
	line := strings.Join(strings.Fields(message), " ")
	if utf8.RuneCountInString(line) <= titleFromRunes {
		return line
	}
	return string([]rune(line)[:titleFromRunes-1]) + "…"
}

// NewSendID names one send, so the archive answers it once.
func NewSendID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "cli-" + hex.EncodeToString(b), nil
}

// chatTarget is the archived conversation a chat writes to. With none of its fields set, the chat
// stays what it has always been: a session of its own that is not archived.
type chatTarget struct {
	start     bool
	proceed   bool
	reference string
}

func (t chatTarget) archived() bool {
	return t.start || t.proceed || t.reference != ""
}

// resolve answers the conversation, starting one titled by the message when there is none to use.
func (t chatTarget) resolve(ask archiveRequest, message string) (string, error) {
	if t.reference != "" {
		return matchConversation(ask, t.reference)
	}
	if t.proceed {
		id, err := latestGeneral(ask)
		if err != nil || id != "" {
			return id, err
		}
	}
	return startConversation(ask, ConversationTitle(message))
}

func shortID(id string) string {
	if len(id) <= shortIDLength {
		return id
	}
	return id[:shortIDLength]
}

func titleOf(item archivedConversation) string {
	if item.Title == "" {
		return "(untitled)"
	}
	return item.Title
}

func printConversations(out io.Writer, page conversationListing) {
	if len(page.Conversations) == 0 {
		fmt.Fprintln(out, "No conversations yet.")
		return
	}
	for _, item := range page.Conversations {
		reading := ""
		if item.Kind != "general" {
			reading = " [reading]"
		}
		updated, _, _ := strings.Cut(item.UpdatedAt, "T")
		fmt.Fprintf(out, "%s  %s · %d turns · %s%s\n", shortID(item.ID), titleOf(item), item.Turns, updated, reading)
	}
	if page.Next != "" {
		fmt.Fprintf(out, "More with --cursor %s\n", page.Next)
	}
}

// printTurns shows each turn in the state the archive holds it, never a reply it does not have.
func printTurns(out io.Writer, reading conversationReading) {
	fmt.Fprintf(out, "%s (%s)\n", titleOf(reading.Conversation), shortID(reading.Conversation.ID))
	for _, turn := range reading.Turns {
		fmt.Fprintf(out, "\nyou: %s\n", turn.Content)
		switch turn.State {
		case "completed":
			fmt.Fprintf(out, "ningo: %s\n", turn.Reply)
			if turn.Provider != "" || turn.Model != "" {
				fmt.Fprintf(out, "(answered by %s)\n", strings.Trim(turn.Provider+" / "+turn.Model, " /"))
			}
		case "failed":
			fmt.Fprintf(out, "(failed: %s)\n", firstNonEmpty(turn.Reason, "no reason recorded"))
		case "interrupted":
			fmt.Fprintln(out, "(interrupted before an answer)")
		default:
			fmt.Fprintln(out, "(still being answered)")
		}
	}
}

// deleteConversation asks first, and names the revision it showed, so a conversation that moved on
// since is refused by the archive instead of being deleted unseen.
func deleteConversation(ask archiveRequest, in io.Reader, out io.Writer, reference string, yes bool) error {
	id, err := matchConversation(ask, reference)
	if err != nil {
		return err
	}
	held, err := readConversation(ask, id, 1)
	if err != nil {
		return err
	}
	name := titleOf(held.Conversation)
	if !yes && !Confirm(in, out, "Delete "+name+"?", []string{
		fmt.Sprintf("%d turns are removed everywhere: desktop, phone and here.", held.Conversation.Turns),
	}) {
		fmt.Fprintln(out, "Nothing was deleted.")
		return nil
	}
	if _, err := ask("conversation.delete", map[string]any{
		"conversation_id": id, "revision": held.Conversation.Revision,
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "Deleted: %s\n", name)
	return nil
}

// retryable finds the last turn that went unanswered, the one the archive will send again.
func retryable(turns []archivedTurn) (archivedTurn, bool) {
	for index := len(turns) - 1; index >= 0; index-- {
		switch turns[index].State {
		case "failed", "interrupted":
			return turns[index], true
		case "completed":
			return archivedTurn{}, false
		}
	}
	return archivedTurn{}, false
}

// stopLatest asks the orchestrator to stop the conversation's latest turn, if it is on its way. It only
// asks: reading the conversation shows how the send ended.
func stopLatest(ask archiveRequest, out io.Writer, reference string) error {
	id, err := matchConversation(ask, reference)
	if err != nil {
		return err
	}
	held, err := readConversation(ask, id, defaultTurnsRead)
	if err != nil {
		return err
	}
	if len(held.Turns) == 0 || held.Turns[len(held.Turns)-1].State != "pending" {
		return fmt.Errorf("nothing in this conversation is on its way")
	}
	latest := held.Turns[len(held.Turns)-1]
	if _, err := ask("conversation.stop", map[string]any{"conversation_id": id, "send_id": latest.SendID}); err != nil {
		return fmt.Errorf("the reply could not be stopped, and it may still arrive: %w", err)
	}
	fmt.Fprintln(out, "Asked Ningo to stop. Read the conversation to see how it ended.")
	return nil
}

func (a *App) newConversationsCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:     "conversations",
		Aliases: []string{"conversation"},
		Short:   "List, read, rename, delete and stop the conversations the desktop and the phone share",
	}
	var limit int
	var cursor string
	list := &cobra.Command{
		Use:   "list",
		Short: "List conversations, most recent first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			page, err := listConversations(a.archiveRequester(ctx), cursor, limit)
			if err != nil {
				return err
			}
			printConversations(cmd.OutOrStdout(), page)
			return nil
		},
	}
	list.Flags().IntVar(&limit, "limit", defaultListed, "how many to list")
	list.Flags().StringVar(&cursor, "cursor", "", "continue a longer list")

	var turns int
	read := &cobra.Command{
		Use:   "read <id>",
		Short: "Show a conversation's turns",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ask := a.archiveRequester(ctx)
			id, err := matchConversation(ask, args[0])
			if err != nil {
				return err
			}
			reading, err := readConversation(ask, id, turns)
			if err != nil {
				return err
			}
			printTurns(cmd.OutOrStdout(), reading)
			return nil
		},
	}
	read.Flags().IntVar(&turns, "limit", defaultTurnsRead, "how many turns to show")

	rename := &cobra.Command{
		Use:   "rename <id> <title>",
		Short: "Rename a conversation",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ask := a.archiveRequester(ctx)
			id, err := matchConversation(ask, args[0])
			if err != nil {
				return err
			}
			held, err := readConversation(ask, id, 1)
			if err != nil {
				return err
			}
			title := joinWords(args[1:])
			if _, err := ask("conversation.rename", map[string]any{
				"conversation_id": id, "revision": held.Conversation.Revision, "title": title,
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed to: %s\n", title)
			return nil
		},
	}

	var yes bool
	remove := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a conversation, after asking",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteConversation(a.archiveRequester(ctx), a.stdin, cmd.OutOrStdout(), args[0], yes)
		},
	}
	remove.Flags().BoolVar(&yes, "yes", false, "do not ask")

	retry := &cobra.Command{
		Use:   "retry <id>",
		Short: "Send a conversation's last unanswered turn again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ask := a.archiveRequester(ctx)
			id, err := matchConversation(ask, args[0])
			if err != nil {
				return err
			}
			held, err := readConversation(ask, id, defaultTurnsRead)
			if err != nil {
				return err
			}
			turn, ok := retryable(held.Turns)
			if !ok {
				return fmt.Errorf("the last turn of this conversation has nothing to send again")
			}
			return a.chatOnce(ctx, "", chatSessionOptions{retryID: turn.ID, target: chatTarget{reference: id}, conversationID: &id})
		},
	}

	stop := &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop the reply a conversation is waiting for",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopLatest(a.archiveRequester(ctx), cmd.OutOrStdout(), args[0])
		},
	}

	command.AddCommand(list, read, rename, remove, retry, stop)
	return command
}
