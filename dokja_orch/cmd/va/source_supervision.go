package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"read_books/internal/handlers"
	applicationjournal "read_books/internal/infrastructure/application_journal"
	"read_books/internal/logger"
	"read_books/internal/sourceaccess"
)

type sourceJournalConnection struct {
	db    *sql.DB
	store *applicationjournal.Store
}

// This host is optional. Configuration and storage failures disable only its
// controls; the rest of the orchestrator must still be able to start.
func configureSourceSupervision(lifetime context.Context, handler *handlers.KnowledgeDomainHandler, enabled func() bool, token, dsn string, otherCredentials ...string) func() {
	journal, err := openSourceJournal(token, dsn, otherCredentials...)
	if err != nil {
		logger.Error("Desktop source supervision disabled; continuing orchestrator startup", err)
		return func() {}
	}
	if journal == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(lifetime)
	handler.WithSourceApplications(handlers.NewDesktopSourceApplications(ctx, handler, journal.store, enabled))
	return func() { cancel(); journal.db.Close() }
}

// Startup checks an existing namespace; only the separate explicit init command
// creates journal tables. Connection failures never expose DSNs or native errors.
func openSourceJournal(token, dsn string, otherCredentials ...string) (*sourceJournalConnection, error) {
	if token == "" && dsn == "" {
		return nil, nil
	}
	for _, other := range otherCredentials {
		if token != "" && token == other {
			return nil, fmt.Errorf("source supervision requires a dedicated operator credential")
		}
	}
	if !sourceaccess.ValidToken(token) || dsn == "" {
		return nil, fmt.Errorf("Desktop source supervision requires a valid dedicated credential and PostgreSQL DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("source journal connection configuration is invalid")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("source journal PostgreSQL connection is unavailable; check database availability and connection configuration")
	}
	store, err := applicationjournal.Open(ctx, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("source journal namespace check failed; verify availability and schema version, and initialize explicitly if missing")
	}
	return &sourceJournalConnection{db, store}, nil
}
