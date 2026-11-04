// Explicit operator command: creates only the application journal namespace.
package main

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	applicationjournal "read_books/internal/infrastructure/application_journal"
	"time"
)

func main() {
	dsn := os.Getenv("DOKJA_SOURCE_SUPERVISION_POSTGRES_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DOKJA_SOURCE_SUPERVISION_POSTGRES_DSN is required")
		os.Exit(1)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid journal connection configuration")
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := applicationjournal.Initialize(ctx, db); err != nil {
		fmt.Fprintln(os.Stderr, "Journal initialization failed")
		os.Exit(1)
	}
	fmt.Println("Application journal namespace initialized")
}
