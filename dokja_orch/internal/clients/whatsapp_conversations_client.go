package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	communications "read_books/dokja_domain/dokja_communications"
)

const (
	conversationsTimeout   = 8 * time.Second
	conversationsMaxBody   = 512 * 1024
	conversationsPageLimit = 30
	conversationsTextMax   = 4000
)

var (
	conversationAlias  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	conversationCursor = regexp.MustCompile(`^[0-9]{0,15}$`)
	conversationID     = regexp.MustCompile(`^[0-9]{1,15}$`)
	conversationWho    = map[string]bool{"owner": true, "bot": true, "contact": true, "member": true}
)

// WhatsAppConversationsClient reads the conversations the owner named on the WhatsApp interface
// and their recorded text, from its internal listener. The interface is trusted, but its reply
// goes on to a screen, so it is cut down to the fields and sizes a page of messages has.
type WhatsAppConversationsClient struct {
	base       string
	httpClient *http.Client
}

// NewWhatsAppConversationsClient derives its routes from the interface's status address. It
// returns nil when there is none.
func NewWhatsAppConversationsClient(statusEndpoint string) *WhatsAppConversationsClient {
	endpoint := strings.TrimSpace(statusEndpoint)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return nil
	}
	base := strings.TrimSuffix(strings.TrimRight(endpoint, "/"), "/status")
	return &WhatsAppConversationsClient{base: base, httpClient: &http.Client{Timeout: conversationsTimeout}}
}

// List returns the named conversations by alias.
func (c *WhatsAppConversationsClient) List(ctx context.Context) ([]communications.Conversation, error) {
	var reply struct {
		Conversations []struct {
			Alias         string `json:"alias"`
			Kind          string `json:"kind"`
			RetentionDays int    `json:"retention_days"`
		} `json:"conversations"`
	}
	if err := c.get(ctx, "/conversations", &reply); err != nil {
		return nil, err
	}
	out := make([]communications.Conversation, 0, len(reply.Conversations))
	for _, entry := range reply.Conversations {
		if !conversationAlias.MatchString(entry.Alias) || (entry.Kind != "direct" && entry.Kind != "group") {
			continue
		}
		days := entry.RetentionDays
		if days < 1 || days > 365 {
			days = 0
		}
		out = append(out, communications.Conversation{Alias: entry.Alias, Kind: entry.Kind, RetentionDays: days})
	}
	return out, nil
}

// History returns one page of a conversation, in the shape of a page of the Discord read service.
func (c *WhatsAppConversationsClient) History(ctx context.Context, alias, before string) (map[string]any, error) {
	if !conversationAlias.MatchString(alias) || !conversationCursor.MatchString(before) {
		return nil, fmt.Errorf("invalid conversation request")
	}
	query := url.Values{"alias": {alias}}
	if before != "" {
		query.Set("before", before)
	}
	var reply struct {
		Messages []struct {
			ID        string `json:"id"`
			Author    string `json:"author"`
			Bot       bool   `json:"bot"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		} `json:"messages"`
		Before string `json:"before"`
	}
	if err := c.get(ctx, "/history?"+query.Encode(), &reply); err != nil {
		return nil, err
	}

	messages := make([]any, 0, len(reply.Messages))
	for _, message := range reply.Messages {
		if len(messages) >= conversationsPageLimit {
			break
		}
		if !conversationID.MatchString(message.ID) || !conversationWho[message.Author] {
			continue
		}
		messages = append(messages, map[string]any{
			"id":          message.ID,
			"author":      message.Author,
			"bot":         message.Author == "bot",
			"content":     printableText(message.Content, conversationsTextMax),
			"timestamp":   printableText(message.Timestamp, 40),
			"edited":      false,
			"reply_to":    "",
			"attachments": []any{},
		})
	}
	next := reply.Before
	if !conversationID.MatchString(next) {
		next = ""
	}
	return map[string]any{"messages": messages, "before": next}, nil
}

// deliverMessages say why a delivery was not made. They are fixed sentences chosen from the status
// code, so nothing of the request or of the interface's reply is ever echoed.
var deliverMessages = map[int]string{
	http.StatusBadRequest:      "the text was not accepted",
	http.StatusNotFound:        "that conversation is not named on the interface",
	http.StatusConflict:        "the WhatsApp account is not connected",
	http.StatusTooManyRequests: "too many messages: wait a moment before sending again",
}

// Deliver sends text into a named conversation, as the linked account. The interface enforces the
// alias, the length and the budget; this only carries the request.
func (c *WhatsAppConversationsClient) Deliver(ctx context.Context, alias, content string) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("whatsapp conversations client is not configured")
	}
	if !conversationAlias.MatchString(alias) || strings.TrimSpace(content) == "" {
		return fmt.Errorf("invalid whatsapp delivery")
	}
	body, err := json.Marshal(map[string]string{"alias": alias, "content": content})
	if err != nil {
		return fmt.Errorf("encode whatsapp delivery: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/deliver", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("build whatsapp delivery: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("the WhatsApp interface did not answer")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, conversationsMaxBody))
	if response.StatusCode == http.StatusOK {
		return nil
	}
	if message, ok := deliverMessages[response.StatusCode]; ok {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("the message could not be sent")
}

func (c *WhatsAppConversationsClient) get(ctx context.Context, path string, into any) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("whatsapp conversations client is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("build conversations request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call conversations: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("conversations returned status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, conversationsMaxBody))
	if err != nil {
		return fmt.Errorf("read conversations: %w", err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("decode conversations: %w", err)
	}
	return nil
}

// printableText keeps line breaks and visible characters, and cuts the text to a limit.
func printableText(text string, limit int) string {
	out := make([]rune, 0, len(text))
	for _, r := range text {
		if len(out) >= limit {
			break
		}
		if r == '\n' || r == '\t' || unicode.IsPrint(r) {
			out = append(out, r)
		}
	}
	return string(out)
}
