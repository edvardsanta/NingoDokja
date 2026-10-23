package runtime

import (
	"strings"

	domain "read_books/dokja_domain/dokja_runtime"
)

// classifyChatFailure is used only after an explicit failed exit or error
// envelope, never on an accepted reply. Diagnostics are bounded by the process
// buffers and discarded; unrecognized text stays a generic failure.
func classifyChatFailure(stdout, stderr []byte) *domain.ChatProviderFailure {
	text := strings.ToLower(string(stdout) + "\n" + string(stderr))
	groups := []struct {
		code    domain.ChatFailureCode
		markers []string
	}{
		{domain.ChatSessionLimit, []string{"session limit", "session_limit", "session budget", "maximum number of sessions"}},
		{domain.ChatUsageLimit, []string{"rate_limit_error", "insufficient_quota", "rate limit", "hit your limit", "reached your usage limit", "credit balance is too low", "credit balance too low"}},
		{domain.ChatAuthentication, []string{"authentication_error", "invalid_api_key", "invalid api key", "not logged in", "login required", "please run /login"}},
		{domain.ChatModelUnavailable, []string{"model_not_found", "unknown model", "model not found", "unsupported model"}},
		{domain.ChatCLIIncompatible, []string{"unknown option", "unrecognized argument", "unexpected argument"}},
	}
	for _, group := range groups {
		for _, marker := range group.markers {
			if strings.Contains(text, marker) {
				return &domain.ChatProviderFailure{Code: group.code}
			}
		}
	}
	return &domain.ChatProviderFailure{Code: domain.ChatUnavailable}
}
