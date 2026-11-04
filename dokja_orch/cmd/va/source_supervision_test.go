package main

import (
	"context"
	"net"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"strings"
	"testing"
)

func TestSourceSupervisionStartupIsExplicitAndSanitized(t *testing.T) {
	if value, err := openSourceJournal("", ""); err != nil || value != nil {
		t.Fatal("disabled feature attempted journal access")
	}
	for _, tc := range []struct{ token, dsn string }{{"", "private-dsn"}, {"CHANGE_ME_" + strings.Repeat("a", 40), "private-dsn"}, {strings.Repeat("a", 40), ""}, {strings.Repeat("a", 40), "host='invalid private-connection"}} {
		_, err := openSourceJournal(tc.token, tc.dsn)
		if err == nil {
			t.Fatal("invalid config accepted")
		}
		if strings.Contains(err.Error(), "private-") {
			t.Fatal("native connection details leaked")
		}
	}
}

func TestSourceSupervisionRejectsSharedServiceCredential(t *testing.T) {
	token := strings.Repeat("s", 40)
	if _, err := openSourceJournal(token, "private-dsn", token); err == nil {
		t.Fatal("service credential granted operator scope")
	}
}

// A real failed TCP handshake exercises pgx without needing a PostgreSQL server.
func unavailableJournalDSN(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			connection.Close()
		}
	}()
	return "postgres://private-user:private-password@" + listener.Addr().String() + "/private-database?sslmode=disable&connect_timeout=1"
}

func TestUnavailableSourceJournalReportsConnectionFailure(t *testing.T) {
	journal, err := openSourceJournal(strings.Repeat("a", 40), unavailableJournalDSN(t))
	if journal != nil || err == nil {
		t.Fatal("unavailable database was accepted")
	}
	if !strings.Contains(err.Error(), "connection is unavailable") || strings.Contains(err.Error(), "initialize") || strings.Contains(err.Error(), "private-") {
		t.Fatalf("connection failure was misleading or exposed credentials: %v", err)
	}
}

type startupKnowledgeService struct{ calls int }

func (s *startupKnowledgeService) Dispatch(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	s.calls++
	return map[string]any{"ok": true}, nil
}

func TestOptionalSourceSupervisionFailureLeavesOrchestratorUsable(t *testing.T) {
	token := strings.Repeat("a", 40)
	for _, tc := range []struct{ name, token, dsn, other string }{
		{"disabled", "", "", ""},
		{"partial", token, "", ""},
		{"invalid", token, "host='private-invalid", ""},
		{"shared-credential", token, "private-dsn", token},
		{"unavailable", token, unavailableJournalDSN(t), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &startupKnowledgeService{}
			handler := handlers.NewKnowledgeDomainHandler(backend)
			closeHost := configureSourceSupervision(context.Background(), handler, func() bool { return true }, tc.token, tc.dsn, tc.other)
			defer closeHost()
			orchestrator := core.DefaultService(handler).WithSourceSupervisionToken(token)
			_, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
				Source: core.SourceCLI, Type: "knowledge.search", Payload: map[string]any{"query": "fixture"},
			})
			if err != nil || backend.calls != 1 {
				t.Fatalf("ordinary workflow failed: calls=%d err=%v", backend.calls, err)
			}
			for _, action := range []string{"status", "start", "preview", "control"} {
				_, err = orchestrator.ProcessWithResult(context.Background(), core.Event{
					Source: core.SourceDesktop, Type: "knowledge.application.supervision." + action,
					Context: map[string]any{"source_supervision_token": token},
				})
				if err == nil || !strings.Contains(err.Error(), "source supervision is not configured") {
					t.Fatalf("supervision %s was not refused by disabled host: %v", action, err)
				}
			}
			if backend.calls != 1 {
				t.Fatal("supervision fell through to an unjournaled service")
			}
		})
	}
}
