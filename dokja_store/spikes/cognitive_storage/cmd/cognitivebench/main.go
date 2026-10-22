// cognitivebench runs only synthetic local actions and isolated experiment data.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"read_books/dokja_store/spikes/cognitive_storage/cognitive"
	"time"
)

func main() {
	runs := flag.Int("runs", 100, "independent trials per condition")
	engine := flag.String("engine", "memory", "memory or postgres (COGNITIVE_POSTGRES_DSN)")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var factory cognitive.Factory
	switch *engine {
	case "memory":
		factory = func(context.Context) (cognitive.Store, func(), error) {
			return cognitive.NewMemory(cognitive.Fixture()), func() {}, nil
		}
	case "postgres":
		dsn := os.Getenv("COGNITIVE_POSTGRES_DSN")
		if dsn == "" {
			fmt.Fprintln(os.Stderr, "COGNITIVE_POSTGRES_DSN required")
			os.Exit(2)
		}
		factory = func(ctx context.Context) (cognitive.Store, func(), error) {
			p, err := cognitive.NewPostgres(ctx, dsn, cognitive.Fixture())
			return p, func() {
				if p != nil {
					cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_ = p.Close(cleanup)
				}
			}, err
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown engine")
		os.Exit(2)
	}
	report, err := cognitive.Evaluate(ctx, *engine, *runs, factory)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
