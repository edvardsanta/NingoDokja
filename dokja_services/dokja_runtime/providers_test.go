package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"

	domain "read_books/dokja_domain/dokja_runtime"
)

func discovered(t *testing.T, running bool, output string, runErr error) ([]domain.ProviderObservation, [][]string) {
	t.Helper()
	var calls [][]string
	c := &Compose{Run: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if args[0] == "ps" {
			if running {
				return []byte("aaaaaaaaaaaa"), nil
			}
			return nil, nil
		}
		if args[0] != "exec" {
			t.Fatal(args)
		}
		return []byte(output), runErr
	}, Inspect: func(context.Context, ...string) ([]byte, error) {
		return []byte(`[{"Config":{"Labels":{"com.docker.compose.service":"dokja-ollama"}},"State":{"Running":true}}]`), nil
	}}
	p := ProviderDiscovery{Compose: c, LookPath: func(name string) (string, error) {
		if name == "codex" {
			return "/fixture/bin/codex", nil
		}
		return "", errors.New("missing")
	}}
	return p.Inspect(context.Background()), calls
}
func TestDiscoveryListsOnlyModelsInTheSelectedRunningStack(t *testing.T) {
	rows, calls := discovered(t, true, "NAME ID SIZE MODIFIED\nbge-m3:latest 0123456789ab 1 GB today\nfixture/model:latest abcdef012345 2 GB today\n", nil)
	if rows[0].State != "installed" || rows[1].Reason != "executable_missing" || rows[2].State != "reachable" || len(rows[2].Models) != 2 {
		t.Fatal(rows)
	}
	if len(calls) != 2 || strings.Join(calls[1], " ") != "exec -T dokja-ollama ollama list" {
		t.Fatal(calls)
	}
}
func TestDiscoveryNeverStartsStoppedServicesAndDoesNotLeakFailures(t *testing.T) {
	rows, calls := discovered(t, false, "", nil)
	if rows[2].Reason != "service_stopped" || len(calls) != 1 {
		t.Fatal(rows, calls)
	}
	rows, _ = discovered(t, true, "private failure output", errors.New("private error"))
	if rows[2].Reason != "inspection_failed" || len(rows[2].Models) != 0 {
		t.Fatal(rows)
	}
	rows, _ = discovered(t, true, "unknown new table layout", nil)
	if rows[2].Reason != "invalid_response" {
		t.Fatal(rows)
	}
}
func TestModelListingBoundsAndEmptyCatalog(t *testing.T) {
	names, truncated, ok := parseModelList([]byte("NAME ID SIZE MODIFIED\n"))
	if !ok || len(names) != 0 || truncated {
		t.Fatal(names, truncated, ok)
	}
	names, truncated, ok = parseModelList([]byte("NAME ID SIZE MODIFIED\n" + strings.Repeat("fixture/model 0123456789ab 1GB today\n", 65)))
	if !ok || len(names) != 64 || !truncated {
		t.Fatal(len(names), truncated, ok)
	}
	if _, _, ok = parseModelList([]byte(strings.Repeat("x", 65537))); ok {
		t.Fatal("unbounded output accepted")
	}
	if _, _, ok = parseModelList([]byte("NAME ID SIZE MODIFIED\nbroken\n")); ok {
		t.Fatal("malformed row accepted")
	}
}

func TestDiscoveryCancellationDoesNotExecuteModelListing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	p := ProviderDiscovery{LookPath: func(string) (string, error) { return "", errors.New("missing") }, Compose: &Compose{Run: func(ctx context.Context, args ...string) ([]byte, error) {
		calls++
		if args[0] != "ps" {
			t.Fatal("listing executed after cancellation")
		}
		return nil, ctx.Err()
	}}}
	rows := p.Inspect(ctx)
	if calls != 0 || rows[2].Reason != "timeout" {
		t.Fatal(rows, calls)
	}
}

func TestModelListDoesNotMistakeDiagnosticsForModels(t *testing.T) {
	if _, _, ok := parseModelList([]byte("NAME ID SIZE MODIFIED\nError: service unavailable try again\n")); ok {
		t.Fatal("diagnostic parsed as a model")
	}
}
