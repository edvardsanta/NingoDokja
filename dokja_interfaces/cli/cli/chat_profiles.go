package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	store "read_books/dokja_store"
)

// ResolveDBPath finds the shared database: an explicit path, then DOKJA_DB_FILE, then the
// .dokja folder of the repository (searched upward from the working directory).
func ResolveDBPath(flagValue string, env func(string) string, cwd string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if fromEnv := env("DOKJA_DB_FILE"); fromEnv != "" {
		return fromEnv, nil
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(filepath.Join(dir, ".dokja")); err == nil && info.IsDir() {
			return filepath.Join(dir, ".dokja", "dokja.db"), nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", fmt.Errorf("cannot find the shared database: pass --db or set DOKJA_DB_FILE")
}

// tokenSource says where a token comes from. There is deliberately no --token flag: it
// would end up in shell history and in the process list.
type tokenSource struct {
	envName string
	file    string
}

// readToken never echoes the token. With no source given it asks on the terminal (hidden)
// or, when input is piped, reads one line from it.
func readToken(source tokenSource, env func(string) string, stdin *os.File, prompt io.Writer) (string, error) {
	var token string
	switch {
	case source.envName != "":
		token = env(source.envName)
		if strings.TrimSpace(token) == "" {
			return "", fmt.Errorf("environment variable %s is empty or not set", source.envName)
		}
	case source.file != "":
		raw, err := os.ReadFile(source.file)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		if info, err := os.Stat(source.file); err == nil && info.Mode().Perm()&0o077 != 0 {
			fmt.Fprintf(prompt, "warning: %s is readable by other users; consider chmod 600\n", source.file)
		}
		token = string(raw)
	case term.IsTerminal(int(stdin.Fd())):
		fmt.Fprint(prompt, "Token (hidden): ")
		raw, err := term.ReadPassword(int(stdin.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		token = string(raw)
	default:
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read token from input: %w", err)
		}
		token = line
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("the token is empty")
	}
	return token, nil
}

func (a *App) newChatProfileCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "profile",
		Short: "Chat provider profiles (base URL, model and token), switchable without a restart",
		Long: "Profiles live in the shared SQLite database. Creating or removing one writes the file " +
			"directly on this machine: the token never travels through the orchestrator, which can only " +
			"list profiles (masked) and select one. Selecting takes effect on the chat service's next request.",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List profiles with a masked key hint; the active one is marked (chat.profiles.list)",
		RunE: func(cmd *cobra.Command, args []string) error {
			response, err := a.call(ctx, a.baseEvent("chat.profiles.list", map[string]any{}))
			if err != nil {
				return err
			}
			profiles, err := domainField(response.Result, "profiles")
			if err != nil {
				return err
			}
			return printProfileTable(cmd.OutOrStdout(), profiles)
		},
	}

	use := &cobra.Command{
		Use:   "use <name>",
		Short: "Make a profile the active one (chat.profile.use)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.request(ctx, a.baseEvent("chat.profile.use", map[string]any{"name": strings.TrimSpace(args[0])}))
		},
	}

	var dbPath, baseURL, model, tokenEnv, tokenFile string
	var activate bool
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Create or replace a profile (local write; the token is read hidden, from a file or from an env var)",
		Example: "  dokja-cli chat profile add hosted --base-url https://api.example.com/v1 --model example-model\n" +
			"  dokja-cli chat profile add backup --base-url https://api.example.com/v1 --model x --token-env MY_KEY --use",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := readToken(tokenSource{tokenEnv, tokenFile}, os.Getenv, os.Stdin, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			db, path, err := openLocalDB(dbPath)
			if err != nil {
				return err
			}
			defer db.Close()

			name := strings.TrimSpace(args[0])
			if err := db.SaveProfile(store.NewProfile{Name: name, BaseURL: strings.TrimSpace(baseURL), Model: strings.TrimSpace(model), APIKey: token}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile %q saved in %s\n", name, path)
			if activate {
				if err := db.UseProfile(name); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "profile %q is now the active one\n", name)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "to use it: dokja-cli chat profile use %s\n", name)
			}
			return nil
		},
	}
	add.Flags().StringVar(&dbPath, "db", "", "Shared database path (default: DOKJA_DB_FILE or the repository's .dokja/dokja.db)")
	add.Flags().StringVar(&baseURL, "base-url", "", "Provider base URL, e.g. https://api.example.com/v1 (required)")
	add.Flags().StringVar(&model, "model", "", "Model name (required)")
	add.Flags().StringVar(&tokenEnv, "token-env", "", "Read the token from this environment variable")
	add.Flags().StringVar(&tokenFile, "token-file", "", "Read the token from this file")
	add.Flags().BoolVar(&activate, "use", false, "Also make it the active profile")
	_ = add.MarkFlagRequired("base-url")
	_ = add.MarkFlagRequired("model")

	var removeDB string
	var force bool
	remove := &cobra.Command{
		Use:   "remove <name>",
		Short: "Delete a profile (local write); the active one needs --force",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openLocalDB(removeDB)
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.DeleteProfile(strings.TrimSpace(args[0]), force); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile %q removed\n", args[0])
			return nil
		},
	}
	remove.Flags().StringVar(&removeDB, "db", "", "Shared database path")
	remove.Flags().BoolVar(&force, "force", false, "Allow removing the active profile (chat falls back to the environment settings)")

	command.AddCommand(list, use, add, remove)
	return command
}

func openLocalDB(flagValue string) (*store.DB, string, error) {
	cwd, _ := os.Getwd()
	path, err := ResolveDBPath(flagValue, os.Getenv, cwd)
	if err != nil {
		return nil, "", err
	}
	db, err := store.Open(path)
	return db, path, err
}

func printProfileTable(out io.Writer, profiles any) error {
	rows, _ := profiles.([]any)
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ACTIVE\tNAME\tMODEL\tBASE URL\tKEY")
	for _, item := range rows {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		mark := ""
		if active, _ := fields["active"].(bool); active {
			mark = "*"
		}
		hint, _ := fields["key_hint"].(string)
		if hint == "" {
			hint = "(hidden)"
		}
		fmt.Fprintf(writer, "%s\t%v\t%v\t%v\t%s\n", mark, fields["name"], fields["model"], fields["base_url"], hint)
	}
	return writer.Flush()
}
