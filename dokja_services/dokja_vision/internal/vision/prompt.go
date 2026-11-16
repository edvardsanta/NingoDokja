package vision

import (
	"fmt"
	"os"
	"strings"
)

// MaxPromptFileBytes bounds the operator-written prompt override, the same
// way visions.md bounds Ningo's worldview: configuration, not content.
const MaxPromptFileBytes = 8 << 10

// PromptFromFile reads an optional operator prompt. A missing, empty or
// oversized file is an error so the caller can log and keep the built-in
// default instead of serving a half-read prompt.
func PromptFromFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("prompt file path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read vision prompt file: %w", err)
	}
	if len(data) > MaxPromptFileBytes {
		return "", fmt.Errorf("vision prompt file exceeds %d bytes", MaxPromptFileBytes)
	}
	prompt := strings.TrimSpace(string(data))
	if prompt == "" {
		return "", fmt.Errorf("vision prompt file is empty")
	}
	return prompt, nil
}
