package main

import (
	"context"
	"net"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"strings"
	"testing"
)

func TestSourceJournalIsOptionalAndSanitized(t *testing.T) {
	if value, err := openSourceJournal(""); err != nil || value != nil {
		t.Fatal("an unset DSN attempted journal access")
	}
	_, err := openSourceJournal("host='private-invalid")
	if err == nil {
		t.Fatal("invalid config accepted")
	}
	if strings.Contains(err.Error(), "private-") {
		t.Fatal("native connection details leaked")
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
	journal, err := openSourceJournal(unavailableJournalDSN(t))
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

// The former Desktop source supervision requests are gone: with or without a journal, and with
// the retired credential still in someone's environment, they reach no handler and grant nothing.
func TestRemovedSourceSupervisionLeavesOrchestratorUsable(t *testing.T) {
	retired := strings.Repeat("a", 40)
	for _, tc := range []struct{ name, dsn, token string }{
		{"no journal", "", ""},
		{"retired credential only", "", retired},
		{"invalid journal", "host='private-invalid", retired},
		{"unavailable journal", unavailableJournalDSN(t), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, closeJournal := configureSourceJournal(context.Background(), tc.dsn, tc.token)
			defer closeJournal()
			if store != nil {
				t.Fatal("a journal opened without a reachable initialized database")
			}
			backend := &startupKnowledgeService{}
			orchestrator := core.DefaultService(handlers.NewKnowledgeDomainHandler(backend))
			if _, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
				Source: core.SourceCLI, Type: "knowledge.search", Payload: map[string]any{"query": "fixture"},
			}); err != nil || backend.calls != 1 {
				t.Fatalf("ordinary workflow failed: calls=%d err=%v", backend.calls, err)
			}
			for _, action := range []string{"status", "start", "preview", "control"} {
				_, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
					Source: core.SourceDesktop, Type: "knowledge.application.supervision." + action,
					Context: map[string]any{"source_supervision_token": retired},
				})
				if err == nil || !strings.Contains(err.Error(), "source supervision was removed") {
					t.Fatalf("removed supervision %s was not refused: %v", action, err)
				}
			}
			if backend.calls != 1 {
				t.Fatal("a removed supervision request reached the Knowledge service")
			}
		})
	}
}
