package main

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"read_books/internal/handlers"
	"read_books/internal/infrastructure/profiles"
	"read_books/internal/logger"
)

// configureProfiles picks where the owner's Profiles are kept. With DOKJA_PROFILES_POSTGRES_DSN set
// it is the initialized PostgreSQL namespace, and a database that cannot be used disables Profiles
// instead of quietly keeping them elsewhere, so there are never two copies. Unset, they are kept in
// a SQLite file beside the shared database. The failure is logged without the DSN, and the
// orchestrator still starts.
func configureProfiles(dsn, sharedPath string) (handlers.ProfileBook, func()) {
	if dsn == "" {
		book, err := profiles.Open(profiles.PathBeside(sharedPath))
		if err != nil {
			logger.Error("open profile storage; profiles are disabled", err)
			return nil, func() {}
		}
		return book, func() { book.Close() }
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		logger.Info("Profiles are disabled: the connection configuration is invalid")
		return nil, func() {}
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		logger.Info("Profiles are disabled: the database is unavailable; check that it is running and the connection configuration")
		return nil, func() {}
	}
	book, err := profiles.OpenPostgres(ctx, db)
	if err != nil {
		db.Close()
		logger.Info("Profiles are disabled: the namespace is missing or has another schema version; run profiles-init")
		return nil, func() {}
	}
	logger.Info("Profiles are kept in PostgreSQL")
	return book, func() { db.Close() }
}
