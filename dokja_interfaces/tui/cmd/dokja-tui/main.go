package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"dokja_interfaces/cli/cli"
	"dokja_interfaces/tui/tui"
	store "read_books/dokja_store"
)

func main() {
	endpoint := flag.String("request-endpoint", envOrDefault("DOKJA_ORCH_REQUEST_ENDPOINT", cli.DefaultRequestEndpoint), "Orchestrator ZeroMQ request endpoint")
	refresh := flag.Duration("refresh", 5*time.Second, "How often the panel refreshes")
	timeout := flag.Duration("timeout", 2*time.Minute, "How long to wait for one orchestrator answer")
	images := flag.String("images", envOrDefault("DOKJA_TUI_IMAGES", "auto"), "Meme previews: auto, kitty, blocks or off")
	startTab := flag.String("tab", "panel", "Tab to open: panel, memes, discord or history (the Portuguese names painel and historico also work)")
	lang := flag.String("lang", "", "Interface language: en or pt (default: DOKJA_LANG, then the system locale, then en)")
	dbPath := flag.String("db", "", "Shared database (default: DOKJA_DB_FILE or the repo .dokja/dokja.db); enables creating profiles here")
	flag.Parse()

	if *lang != "" {
		tui.SetLanguage(*lang)
	} else {
		tui.SetLanguage(tui.DetectLanguage(os.Getenv))
	}

	output := tui.NewSafeOutput(os.Stdout)
	model := tui.NewModel(tui.NewOrchestratorClient(*endpoint), *refresh, *timeout)
	model.OpenTab(*startTab)
	if db := openProfileStore(*dbPath); db != nil {
		defer db.Close()
		model.SetProfileStore(db)
	}
	model.EnableImages(tui.DetectImageMode(*images, os.Getenv), output, tui.HTTPFetcher)
	model.SetVideoFramer(tui.FFmpegFrames)

	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(output))
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Free what this session uploaded to the terminal.
	for _, id := range model.ImageIDs() {
		_ = output.WriteRaw([]byte(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)))
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// openProfileStore opens the local database if it can be found. Without it the TUI
// still lists and selects profiles through the orchestrator, it just cannot create them.
func openProfileStore(flagValue string) *store.DB {
	cwd, _ := os.Getwd()
	path, err := cli.ResolveDBPath(flagValue, os.Getenv, cwd)
	if err != nil {
		return nil
	}
	db, err := store.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profiles: cannot open the local database:", err)
		return nil
	}
	return db
}
