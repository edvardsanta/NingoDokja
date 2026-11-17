package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
	"unicode/utf8"
)

const MaxSummaryBytes = 4096

var ErrInvalidSummary = errors.New("invalid conversation summary")

type ContextMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ContextCompaction struct {
	PreviousSummary string           `json:"previous_summary"`
	Older           []ContextMessage `json:"older_turns"`
	Recent          []ContextMessage `json:"-"`
}

// PlanContext preserves recent turns verbatim and only replaces older turns after
// a successful summary. The existing message setting limits the raw turn window.
func PlanContext(summary string, messages []ContextMessage, maxMessages int, idle bool) ContextCompaction {
	maxMessages = max(2, maxMessages)
	plan := ContextCompaction{PreviousSummary: summary, Recent: messages}
	size := 0
	for _, message := range messages {
		size += len(message.Content)
	}
	if len(messages) == 0 || (!idle && len(messages) < maxMessages-1 && size < 32<<10) {
		return plan
	}
	keep := min(8, maxMessages/2, len(messages)/2)
	// Keep complete recent exchanges when possible, never a lone assistant turn.
	keep -= keep % 2
	cut := len(messages) - keep
	recentBytes := 0
	for _, message := range messages[cut:] {
		recentBytes += len(message.Content)
	}
	for recentBytes > 16<<10 && cut < len(messages) {
		recentBytes -= len(messages[cut].Content)
		cut++
	}
	if cut < len(messages) && messages[cut].Role == "assistant" {
		cut++
	}
	plan.Older, plan.Recent = messages[:cut], messages[cut:]
	return plan
}

func summaryInstructions() string { return promptfiles.MustRead("chat_summary_instructions") }

func (p ContextCompaction) SummaryMessages() []ContextMessage {
	data, _ := json.Marshal(p)
	return []ContextMessage{{Role: "system", Content: summaryInstructions()}, {Role: "user", Content: string(data)}}
}

func ValidateSummary(summary string) (string, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" || len(summary) > MaxSummaryBytes || !utf8.ValidString(summary) {
		return "", ErrInvalidSummary
	}
	return summary, nil
}

// ParseSummaryResponse keeps ordinary conversational replies out of the checkpoint.
// This validates the response format, not the factual completeness of model output.
func ParseSummaryResponse(response string) (string, error) {
	var result struct {
		Kind    string `json:"kind"`
		Summary string `json:"summary"`
	}
	if !utf8.ValidString(response) || json.Unmarshal([]byte(response), &result) != nil || result.Kind != "conversation_summary" {
		return "", ErrInvalidSummary
	}
	return ValidateSummary(result.Summary)
}

// SummaryContext is user-level data, never an instruction or a forged assistant turn.
func SummaryContext(summary string) ContextMessage {
	data, _ := json.Marshal(summary)
	return ContextMessage{Role: "user", Content: fmt.Sprintf(promptfiles.MustRead("context_summarycontext1"), data)}
}

// RecoveryContext projects a bounded recent window without changing the durable
// history. The current user message is appended separately and is never trimmed.
func RecoveryContext(messages []ContextMessage, maxMessages int) ([]ContextMessage, bool) {
	keep := min(8, max(1, maxMessages-1))
	cut := max(0, len(messages)-keep)
	size := 0
	for _, message := range messages[cut:] {
		size += len(message.Content)
	}
	for size > 16<<10 && cut < len(messages) {
		size -= len(messages[cut].Content)
		cut++
	}
	// A retained exchange starts with its user turn, not a detached assistant reply.
	if cut < len(messages) && messages[cut].Role == "assistant" {
		cut++
	}
	return append([]ContextMessage(nil), messages[cut:]...), cut > 0
}

func OmittedContextNotice() ContextMessage {
	return ContextMessage{Role: "user", Content: promptfiles.MustRead("context_omittedcontextnotice1")}
}
