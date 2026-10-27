package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	domain "read_books/dokja_domain/dokja_runtime"
)

// drawnPull is a two-layer pull drawn the way the CLI draws without a terminal (see
// testdata/ollama_pull_missing.txt): each frame goes back to the first column, no line breaks.
var drawnPull = strings.Join([]string{
	"\x1b[?2026h\x1b[?25l\x1b[1Gpulling manifest ⠋ \x1b[K\x1b[?25h\x1b[?2026l",
	"\x1b[?2026h\x1b[?25l\x1b[1Gpulling 2bada8a74506:  25% ▕███          ▏ 1.0 GB/4.0 GB  80 MB/s  37s\x1b[K\x1b[?25h\x1b[?2026l",
	"\x1b[?2026h\x1b[?25l\x1b[1Gpulling 2bada8a74506: 100% ▕█████████████▏ 4.0 GB\x1b[K\x1b[?25h\x1b[?2026l",
	"\x1b[?2026h\x1b[?25l\x1b[A\x1b[1Gpulling 66b9ea09bd5b:  50% ▕██████       ▏ 500 MB/1.0 GB\x1b[K",
	"\x1b[1Gpulling 66b9ea09bd5b: 100% ▕█████████████▏ 1.0 GB\x1b[K",
	"\x1b[1Gverifying sha256 digest \x1b[K",
	"\x1b[1Gwriting manifest \x1b[K",
	"\x1b[1Gsuccess \x1b[K\x1b[?25h\x1b[?2026l\n",
}, "")

func streaming(output string, fail error, seen *[][]string) *Compose {
	return &Compose{
		Stream: func(_ context.Context, w io.Writer, args ...string) error {
			*seen = append(*seen, args)
			// Written in pieces that cut lines anywhere, as a pipe hands them over.
			for len(output) > 0 {
				n := min(7, len(output))
				w.Write([]byte(output[:n]))
				output = output[n:]
			}
			return fail
		},
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			*seen = append(*seen, args)
			return nil, nil
		},
	}
}

func TestPullReportsPhasesAndTheShareOverEveryLayer(t *testing.T) {
	var seen [][]string
	var reports []domain.PullProgress
	err := OllamaModels{Compose: streaming(drawnPull, nil, &seen)}.Pull(context.Background(), "qwen3:8b", func(p domain.PullProgress) { reports = append(reports, p) })
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.PullProgress{{Phase: "starting", Percent: -1}, {Phase: "downloading", Percent: 25}, {Phase: "downloading", Percent: 100}, {Phase: "downloading", Percent: 90}, {Phase: "downloading", Percent: 100}, {Phase: "verifying", Percent: -1}, {Phase: "writing", Percent: -1}}
	if len(reports) != len(want) {
		t.Fatal(reports)
	}
	for i := range want {
		if reports[i] != want[i] {
			t.Fatal(i, reports)
		}
	}
	args := strings.Join(seen[0], " ")
	if !strings.HasPrefix(args, "exec -T dokja-ollama sh -c ") || seen[0][len(seen[0])-1] != "qwen3:8b" {
		t.Fatal(seen[0])
	}
}

func TestPullSaysWhenTheRegistryHasNoSuchModel(t *testing.T) {
	var seen [][]string
	err := OllamaModels{Compose: streaming("pulling manifest \nError: pull model manifest: file does not exist\n", errors.New("exit 1"), &seen)}.Pull(context.Background(), "no-such", func(domain.PullProgress) {})
	if !errors.Is(err, domain.ErrModelNotFound) {
		t.Fatal(err)
	}
}

func TestTheExitStatusSaysWhetherThePullWorked(t *testing.T) {
	var seen [][]string
	if err := (OllamaModels{Compose: streaming("whatever the engine draws", nil, &seen)}).Pull(context.Background(), "qwen3:8b", func(domain.PullProgress) {}); err != nil {
		t.Fatal("a pull that exits 0 worked, whatever it drew", err)
	}
	err := OllamaModels{Compose: streaming("pulling manifest \n", errors.New("exit 1"), &seen)}.Pull(context.Background(), "qwen3:8b", func(domain.PullProgress) {})
	if err == nil || errors.Is(err, domain.ErrModelNotFound) {
		t.Fatal(err)
	}
}

// Recorded from the real CLI without a terminal: redraws with escapes and no line breaks.
func TestTheRealCLIOutputForAMissingModelIsRead(t *testing.T) {
	raw, err := os.ReadFile("testdata/ollama_pull_missing.txt")
	if err != nil {
		t.Fatal(err)
	}
	var seen [][]string
	var reports []domain.PullProgress
	err = OllamaModels{Compose: streaming(string(raw), errors.New("exit 1"), &seen)}.Pull(context.Background(), "missing", func(p domain.PullProgress) { reports = append(reports, p) })
	if !errors.Is(err, domain.ErrModelNotFound) || len(reports) == 0 || reports[0].Phase != "starting" {
		t.Fatal(err, reports)
	}
}

func TestACancelledPullIsStoppedInsideTheContainerToo(t *testing.T) {
	var seen [][]string
	ctx, cancel := context.WithCancel(context.Background())
	compose := streaming("", nil, &seen)
	compose.Stream = func(ctx context.Context, _ io.Writer, args ...string) error {
		seen = append(seen, args)
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}
	err := OllamaModels{Compose: compose}.Pull(ctx, "qwen3:8b", func(domain.PullProgress) {})
	if !errors.Is(err, context.Canceled) || len(seen) != 2 || !strings.Contains(strings.Join(seen[1], " "), "kill") {
		t.Fatal(err, seen)
	}
}

func TestRemoveRunsTheEnginesRemoveAndSaysWhenItHasNoSuchModel(t *testing.T) {
	var seen [][]string
	if err := (OllamaModels{Compose: streaming("deleted 'qwen3:8b'\n", nil, &seen)}).Remove(context.Background(), "qwen3:8b"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen[0], " ") != "exec -T dokja-ollama ollama rm qwen3:8b" {
		t.Fatal(seen[0])
	}
	err := OllamaModels{Compose: streaming("Error: model 'x' not found\n", errors.New("exit 1"), &seen)}.Remove(context.Background(), "x")
	if !errors.Is(err, domain.ErrModelNotFound) {
		t.Fatal(err)
	}
}

func TestEmbeddingModelComesFromTheStacksSettings(t *testing.T) {
	if (OllamaModels{}).EmbeddingModel() != "bge-m3" {
		t.Fatal("the stack's default is bge-m3")
	}
	set := OllamaModels{Environment: func(env []string) ([]string, error) { return append(env, "DOKJA_EMBED_MODEL=nomic-embed-text"), nil }}
	if set.EmbeddingModel() != "nomic-embed-text" {
		t.Fatal(set.EmbeddingModel())
	}
	odd := OllamaModels{Environment: func(env []string) ([]string, error) { return append(env, "DOKJA_EMBED_MODEL=--rm -rf"), nil }}
	if odd.EmbeddingModel() != "bge-m3" {
		t.Fatal(odd.EmbeddingModel())
	}
}

// Regression: a failed download said "check the network and the free disk space" whatever the
// engine had said. The engine's own reason now travels with the failure.
func TestAFailedPullCarriesTheEnginesReason(t *testing.T) {
	var seen [][]string
	output := "pulling manifest \nverifying sha256 digest \nError: digest mismatch, file must be downloaded again: " + strings.Repeat("x", 300) + "\n"
	err := OllamaModels{Compose: streaming(output, errors.New("exit 1"), &seen)}.Pull(context.Background(), "qwen3:8b", func(domain.PullProgress) {})
	var failure domain.PullFailure
	if !errors.As(err, &failure) || !strings.HasPrefix(failure.Detail, "digest mismatch, file must be downloaded again") {
		t.Fatal(err)
	}
	if len([]rune(failure.Detail)) > 201 || strings.ContainsAny(failure.Detail, "\n\r") {
		t.Fatal("the reason is kept short and on one line", failure.Detail)
	}
	// With nothing said, the failure stays plain.
	err = OllamaModels{Compose: streaming("pulling manifest \n", errors.New("exit 1"), &seen)}.Pull(context.Background(), "qwen3:8b", func(domain.PullProgress) {})
	if errors.As(err, &failure) {
		t.Fatal("no reason was given", err)
	}
}
