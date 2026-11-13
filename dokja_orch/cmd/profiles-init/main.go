// Explicit operator command: creates only the profiles namespace of the desktop's Profiles.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"read_books/internal/infrastructure/profiles"
)

func main() {
	dsn := os.Getenv("DOKJA_PROFILES_POSTGRES_DSN")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DOKJA_PROFILES_POSTGRES_DSN is required")
		os.Exit(1)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid profiles connection configuration")
		os.Exit(1)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := profiles.InitializePostgres(ctx, db); err != nil {
		fmt.Fprintln(os.Stderr, "Profiles initialization failed")
		os.Exit(1)
	}
	fmt.Println("Profiles namespace initialized")
}
