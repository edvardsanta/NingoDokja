package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "read_books/dokja_store"
)

const cliSecret = "sk-or-v1-SUPERSECRETTOKEN-wxyz7890"

// capture runs fn with stdout and stderr redirected and returns what was printed.
func capture(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	reader, writer, _ := os.Pipe()
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = writer, writer
	err := fn()
	writer.Close()
	os.Stdout, os.Stderr = stdout, stderr
	out, _ := io.ReadAll(reader)
	return string(out), err
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return capture(t, func() error {
		return NewApp().Run(context.Background(), append([]string{"dokja-cli"}, args...))
	})
}

func tokenFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveDBPathPrefersFlagThenEnvThenTheRepoFolder(t *testing.T) {
	env := func(map[string]string) func(string) string {
		return func(string) string { return "" }
	}(nil)
	withEnv := func(key string) string {
		if key == "DOKJA_DB_FILE" {
			return "/from/env.db"
		}
		return ""
	}

	if path, _ := ResolveDBPath("/from/flag.db", withEnv, "/x"); path != "/from/flag.db" {
		t.Fatalf("the flag wins, got %q", path)
	}
	if path, _ := ResolveDBPath("", withEnv, "/x"); path != "/from/env.db" {
		t.Fatalf("then the environment, got %q", path)
	}

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".dokja"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "dokja_interfaces", "cli")
	_ = os.MkdirAll(nested, 0o755)
	path, err := ResolveDBPath("", env, nested)
	if err != nil || path != filepath.Join(repo, ".dokja", "dokja.db") {
		t.Fatalf("expected the repo's .dokja folder, got %q %v", path, err)
	}

	if _, err := ResolveDBPath("", env, t.TempDir()); err == nil {
		t.Fatal("no database anywhere must be an error")
	}
}

func TestReadTokenFromEnvFileAndPipedInput(t *testing.T) {
	env := func(key string) string {
		return map[string]string{"MY_KEY": "  " + cliSecret + "\n", "EMPTY": "  "}[key]
	}

	got, err := readToken(tokenSource{envName: "MY_KEY"}, env, os.Stdin, io.Discard)
	if err != nil || got != cliSecret {
		t.Fatalf("env token should be trimmed, got %q %v", got, err)
	}
	if _, err := readToken(tokenSource{envName: "EMPTY"}, env, os.Stdin, io.Discard); err == nil {
		t.Fatal("an empty variable must be refused")
	}
	if _, err := readToken(tokenSource{envName: "UNSET"}, env, os.Stdin, io.Discard); err == nil {
		t.Fatal("an unset variable must be refused")
	}

	got, err = readToken(tokenSource{file: tokenFile(t, cliSecret+"\n")}, env, os.Stdin, io.Discard)
	if err != nil || got != cliSecret {
		t.Fatalf("file token should be trimmed, got %q %v", got, err)
	}
	if _, err := readToken(tokenSource{file: tokenFile(t, " \n")}, env, os.Stdin, io.Discard); err == nil {
		t.Fatal("an empty file must be refused")
	}
	if _, err := readToken(tokenSource{file: filepath.Join(t.TempDir(), "missing")}, env, os.Stdin, io.Discard); err == nil {
		t.Fatal("a missing file must be refused")
	}

	pipe, writer, _ := os.Pipe()
	writer.WriteString(cliSecret + "\nsecond line\n")
	writer.Close()
	got, err = readToken(tokenSource{}, env, pipe, io.Discard)
	if err != nil || got != cliSecret {
		t.Fatalf("piped input reads the first line, got %q %v", got, err)
	}
}

func TestReadTokenWarnsAboutAWorldReadableFile(t *testing.T) {
	path := tokenFile(t, cliSecret)
	_ = os.Chmod(path, 0o644)
	var warning strings.Builder
	if _, err := readToken(tokenSource{file: path}, func(string) string { return "" }, os.Stdin, &warning); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warning.String(), "readable by other users") {
		t.Fatalf("expected a permissions warning, got %q", warning.String())
	}
}

func TestAddSavesAProfileWithoutEverPrintingTheToken(t *testing.T) {
	db := filepath.Join(t.TempDir(), "dokja.db")

	out, err := runCLI(t, "chat", "profile", "add", "hosted", "--db", db,
		"--base-url", "https://api.example.com/v1", "--model", "llama", "--token-file", tokenFile(t, cliSecret), "--use")
	if err != nil {
		t.Fatalf("add failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "SUPERSECRET") || strings.Contains(out, cliSecret) {
		t.Fatalf("the token must never be printed:\n%s", out)
	}

	handle, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	profiles, _ := handle.ListProfiles()
	if len(profiles) != 1 || !profiles[0].Active || profiles[0].KeyHint != "…7890" || profiles[0].Model != "llama" {
		t.Fatalf("unexpected stored profile %+v", profiles)
	}
}

func TestThereIsNoTokenFlag(t *testing.T) {
	out, err := runCLI(t, "chat", "profile", "add", "x", "--db", filepath.Join(t.TempDir(), "d.db"),
		"--base-url", "https://x.example", "--model", "m", "--token", cliSecret)
	if err == nil {
		t.Fatal("a --token flag would leak into shell history and must not exist")
	}
	if strings.Contains(err.Error(), cliSecret) {
		t.Fatalf("the error must not echo the token: %v\n%s", err, out)
	}
}

func TestAddRejectsBadInputWithoutStoringAnything(t *testing.T) {
	db := filepath.Join(t.TempDir(), "dokja.db")
	file := tokenFile(t, cliSecret)
	for name, args := range map[string][]string{
		"bad url":     {"chat", "profile", "add", "a", "--db", db, "--base-url", "ftp://x", "--model", "m", "--token-file", file},
		"bad name":    {"chat", "profile", "add", "Bad Name", "--db", db, "--base-url", "https://x.example", "--model", "m", "--token-file", file},
		"no model":    {"chat", "profile", "add", "a", "--db", db, "--base-url", "https://x.example", "--token-file", file},
		"no base url": {"chat", "profile", "add", "a", "--db", db, "--model", "m", "--token-file", file},
	} {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	handle, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if profiles, _ := handle.ListProfiles(); len(profiles) != 0 {
		t.Fatalf("nothing may be stored, got %+v", profiles)
	}
}

func TestRemoveProtectsTheActiveProfileUnlessForced(t *testing.T) {
	db := filepath.Join(t.TempDir(), "dokja.db")
	file := tokenFile(t, cliSecret)
	for _, name := range []string{"one", "two"} {
		args := []string{"chat", "profile", "add", name, "--db", db, "--base-url", "https://x.example", "--model", "m", "--token-file", file}
		if name == "one" {
			args = append(args, "--use")
		}
		if out, err := runCLI(t, args...); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
	}

	if _, err := runCLI(t, "chat", "profile", "remove", "one", "--db", db); err == nil {
		t.Fatal("removing the active profile needs --force")
	}
	if out, err := runCLI(t, "chat", "profile", "remove", "two", "--db", db); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := runCLI(t, "chat", "profile", "remove", "one", "--db", db, "--force"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestProfileTableMasksTheKeyAndMarksTheActiveOne(t *testing.T) {
	var out strings.Builder
	err := printProfileTable(&out, []any{
		map[string]any{"name": "hosted", "model": "llama", "base_url": "https://api.example.com/v1", "key_hint": "…7890", "active": true},
		map[string]any{"name": "short", "model": "m", "base_url": "https://s.example", "key_hint": "", "active": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"ACTIVE", "*", "hosted", "…7890", "(hidden)"} {
		if !strings.Contains(text, want) {
			t.Errorf("table is missing %q:\n%s", want, text)
		}
	}
}
