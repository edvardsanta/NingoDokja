// Explicit operator command: creates only the price history namespace (ADR 0015).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	pricehistory "read_books/internal/infrastructure/price_history"
)

func main() {
	dsn := os.Getenv("DOKJA_PRICE_POSTGRES_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DOKJA_PRICE_POSTGRES_DSN is required")
		os.Exit(1)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid price history connection configuration")
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := pricehistory.Initialize(ctx, db); err != nil {
		fmt.Fprintln(os.Stderr, "Price history initialization failed")
		os.Exit(1)
	}
	fmt.Println("Price history namespace initialized")
}
