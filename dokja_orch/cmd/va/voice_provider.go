package main

import (
	"fmt"
	"read_books/internal/clients"
	"read_books/internal/handlers"
	"strings"
)

// Runtime is the default: local voice inference requires an explicit selection.
func voiceGeneratorForProvider(provider, endpoint string) (handlers.VoiceChatStreamingGenerator, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "runtime":
		return nil, nil
	case "ollama":
		return clients.NewVoiceChatClient(endpoint), nil
	default:
		return nil, fmt.Errorf("unsupported voice chat provider %q", provider)
	}
}
