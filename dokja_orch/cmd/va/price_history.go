package main

import (
	"context"
	"database/sql"

	pricehistory "read_books/internal/infrastructure/price_history"
)

// preparePriceHistory reopens a reachable namespace, or uses the established
// transactional initializer to repair an absent or supported older schema.
// Connection errors remain behind the status code boundary in price_buffer.go.
func preparePriceHistory(ctx context.Context, db *sql.DB) (*pricehistory.Store, error) {
	target, err := pricehistory.Open(ctx, db)
	if err != nil && ctx.Err() == nil {
		return pricehistory.Initialize(ctx, db)
	}
	return target, err
}
