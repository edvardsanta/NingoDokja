package main

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	pricedomain "read_books/dokja_domain/dokja_price"
	pricehistory "read_books/internal/infrastructure/price_history"
	"read_books/internal/logger"
)

// configurePriceHistory picks where the Price domain keeps its history (ADR 0015). With
// DOKJA_PRICE_POSTGRES_DSN set, it is the initialized PostgreSQL namespace. Unset, or when that
// database cannot be used, history stays in memory and a restart forgets it; the failure is
// logged without the DSN, and the orchestrator still starts.
func configurePriceHistory(dsn string) (pricedomain.Repository, func()) {
	if dsn == "" {
		logger.Info("Price history is kept in memory: DOKJA_PRICE_POSTGRES_DSN is unset")
		return pricedomain.NewMemoryRepository(), func() {}
	}
	store, db, reason := openPriceHistory(dsn)
	if store == nil {
		logger.Info("Price history is kept in memory, so a restart forgets it: " + reason)
		return pricedomain.NewMemoryRepository(), func() {}
	}
	logger.Info("Price history is kept in PostgreSQL")
	return store, func() { db.Close() }
}

// openPriceHistory never returns native errors, which could carry the DSN.
func openPriceHistory(dsn string) (*pricehistory.Store, *sql.DB, string) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, "the connection configuration is invalid"
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, "the database is unavailable; check that it is running and the connection configuration"
	}
	store, err := pricehistory.Open(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, "the namespace is missing or has another schema version; run price-history-init"
	}
	return store, db, ""
}
