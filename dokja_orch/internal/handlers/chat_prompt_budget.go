package handlers

import (
	"fmt"
	"strings"
)

// chatPromptBudget stays under the runtime's hard cap (dokja_runtime validates
// 64 KiB of history per turn) with margin for the provider's own framing.
const chatPromptBudget = 60 * 1024

// fitChatPrompt drops the oldest conversation turns when the assembled prompt would
// exceed the runtime's supported size, so a big attachment or a long session degrades
// into a shorter memory instead of a silent failure. System messages (persona,
// references, evidence) and the final user turn always stay.
func fitChatPrompt(messages []map[string]string) []map[string]string {
	size := chatHistorySize(messages)
	if size <= chatPromptBudget {
		return messages
	}
	trimmed := make([]map[string]string, len(messages))
	copy(trimmed, messages)
	for size > chatPromptBudget && len(trimmed) > 1 {
		dropped := false
		// The last message is the current turn: it is never dropped.
		for i := 0; i < len(trimmed)-1; i++ {
			role := trimmed[i]["role"]
			if role != "user" && role != "assistant" {
				continue
			}
			size -= len(trimmed[i]["content"])
			trimmed = append(trimmed[:i], trimmed[i+1:]...)
			dropped = true
			break
		}
		if !dropped {
			break
		}
	}
	if len(trimmed) < len(messages) {
		trimmed = append([]map[string]string{{"role": "system", "content": fmt.Sprintf(
			"The earlier conversation exceeded the supported size; the oldest turns were left out (%d dropped).", len(messages)-len(trimmed))}}, trimmed...)
	}
	return trimmed
}

// chatHistorySize sums the content bytes the runtime will measure.
func chatHistorySize(messages []map[string]string) int {
	size := 0
	for _, message := range messages {
		size += len(message["content"])
	}
	return size
}

// promptSizeRefused mirrors the runtime's refusal so the retry can recognize it
// without importing the runtime domain's error text beyond this marker.
func promptSizeRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "exceeds supported size")
}

// halveChatHistory drops the older half of the conversation turns, keeping the
// final one, for the single retry after a size refusal.
func halveChatHistory(messages []map[string]string) ([]map[string]string, bool) {
	turns := 0
	for _, message := range messages {
		if message["role"] == "user" || message["role"] == "assistant" {
			turns++
		}
	}
	if turns < 2 {
		return messages, false
	}
	keepFrom := turns / 2
	seen := 0
	out := make([]map[string]string, 0, len(messages))
	dropped := false
	for _, message := range messages {
		if message["role"] == "user" || message["role"] == "assistant" {
			if seen < keepFrom {
				seen++
				dropped = true
				continue
			}
			seen++
		}
		out = append(out, message)
	}
	return out, dropped
}
