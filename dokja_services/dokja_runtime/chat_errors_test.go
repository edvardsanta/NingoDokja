package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "read_books/dokja_domain/dokja_runtime"
)

func TestChatFailureCategoriesNeverExposeDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		diagnostic string
		code       domain.ChatFailureCode
	}{
		{"Session limit reached", domain.ChatSessionLimit},
		{"maximum number of sessions reached", domain.ChatSessionLimit},
		{"rate_limit_error", domain.ChatUsageLimit},
		{"credit balance is too low", domain.ChatUsageLimit},
		{"authentication_error", domain.ChatAuthentication},
		{"Please run /login", domain.ChatAuthentication},
		{"model_not_found", domain.ChatModelUnavailable},
		{"unknown option --fixture", domain.ChatCLIIncompatible},
		{"unexpected provider failure", domain.ChatUnavailable},
	} {
		failure := classifyChatFailure([]byte(tc.diagnostic+" private-output-marker"), []byte("private-error-marker"))
		if failure.Code != tc.code || strings.Contains(failure.Error(), "private-") || strings.Contains(failure.Error(), "--fixture") {
			t.Fatal(failure.Code, failure.Error())
		}
	}
	unknown := &domain.ChatProviderFailure{Code: "private-code-marker"}
	if strings.Contains(unknown.Error(), "private-") {
		t.Fatal("unknown code leaked")
	}
}

func TestClaudeErrorEnvelopeIsNotAnAssistantReply(t *testing.T) {
	for _, isError := range []bool{true, false} {
		a := ChatAdapters{Run: func(context.Context, string, []string, string, string) ([]byte, error) {
			if isError {
				return []byte(`{"is_error":true,"result":"Session limit reached private-error-marker"}`), nil
			}
			return []byte(`{"is_error":false,"result":"Explaining a session limit is a valid assistant answer."}`), nil
		}}
		reply, err := a.Generate(t.Context(), domain.ChatBinding{Provider: "claude_code", Model: "fixture"}, nil)
		if !isError {
			if err != nil || reply == "" {
				t.Fatal(reply, err)
			}
			continue
		}
		var failure *domain.ChatProviderFailure
		if !errors.As(err, &failure) || failure.Code != domain.ChatSessionLimit || reply != "" || strings.Contains(err.Error(), "private-") {
			t.Fatal(reply, err)
		}
	}
}

func TestActualClaudeFailureExitKeepsOnlySafeCategory(t *testing.T) {
	for _, mode := range []string{"stdout", "stderr", "stderr_overflow"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\ncat >/dev/null\n"
			switch mode {
			case "stdout":
				script += "printf '%s\\n' '{\"is_error\":true,\"result\":\"Session limit reached private-output-marker\"}'\n"
			case "stderr":
				script += "printf '%s\\n' 'Session limit reached private-error-marker' >&2\n"
			case "stderr_overflow":
				script += "head -c 65537 /dev/zero >&2\n"
			}
			script += "exit 1\n"
			if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			reply, err := (ChatAdapters{}).Generate(t.Context(), domain.ChatBinding{Provider: "claude_code", Model: "fixture"}, nil)
			var failure *domain.ChatProviderFailure
			want := domain.ChatSessionLimit
			if mode == "stderr_overflow" {
				want = domain.ChatOutputLimit
			}
			if !errors.As(err, &failure) || failure.Code != want || reply != "" || strings.Contains(err.Error(), "private-") {
				t.Fatal(reply, err)
			}
		})
	}
}

// Hide source WriterTo so this exercises io.Copy's destination fast path.
type chatReaderOnly struct{ io.Reader }

func TestChatOutputCopyCannotBypassItsLimit(t *testing.T) {
	output := &chatOutput{}
	n, err := io.Copy(output, chatReaderOnly{strings.NewReader(strings.Repeat("x", 100000))})
	if err == nil || !output.overflow || len(output.Bytes()) > 65536 || n > 65536 {
		t.Fatal("unbounded process copy", n, len(output.Bytes()), err)
	}
}
