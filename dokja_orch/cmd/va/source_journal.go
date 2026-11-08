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
)

type sourceJournalConnection struct {
	db    *sql.DB
	store *applicationjournal.Store
}

// configureSourceJournal opens the PostgreSQL application journal named by dsn and settles what
// a stopped orchestrator left in it. The journal is optional: an unset DSN leaves it off, and a
// configuration or storage failure disables only the journal, never the orchestrator's start.
// retiredToken is the former Desktop operator credential, which no longer grants anything.
func configureSourceJournal(lifetime context.Context, dsn, retiredToken string) (*applicationjournal.Store, func()) {
	if retiredToken != "" {
		logger.Info("DOKJA_SOURCE_SUPERVISION_TOKEN is ignored: Desktop source supervision was removed")
	}
	journal, err := openSourceJournal(dsn)
	if err != nil {
		logger.Error("Source journal disabled; continuing orchestrator startup", err)
		return nil, func() {}
	}
	if journal == nil {
		return nil, func() {}
	}
	// Before serving: reads a stopped orchestrator left unsettled are interrupted, never redone.
	recoverCtx, recovered := context.WithTimeout(lifetime, 10*time.Second)
	report, err := handlers.RecoverSourceReads(recoverCtx, journal.store, time.Now().UTC())
	recovered()
	switch {
	case err != nil:
		logger.Error("Source-read recovery did not finish; unsettled reads stay as they were", err)
	case report.Interrupted > 0 || report.Unreadable > 0:
		logger.Info(fmt.Sprintf("Source-read recovery interrupted=%d unreadable=%d", report.Interrupted, report.Unreadable))
	}
	return journal.store, func() { journal.db.Close() }
}

// Startup checks an existing namespace; only the separate explicit init command
// creates journal tables. Connection failures never expose DSNs or native errors.
func openSourceJournal(dsn string) (*sourceJournalConnection, error) {
	if dsn == "" {
		return nil, nil
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
