package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	applicationjournal "read_books/internal/infrastructure/application_journal"
	"read_books/internal/sourceaccess"
)

type sourceJournalConnection struct {
	db    *sql.DB
	store *applicationjournal.Store
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
	store, err := applicationjournal.Open(ctx, db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("source journal unavailable or uninitialized; initialize its namespace explicitly")
	}
	return &sourceJournalConnection{db, store}, nil
}
