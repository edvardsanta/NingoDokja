package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: 128, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestDetectImageMode(t *testing.T) {
	cases := []struct {
		name    string
		setting string
		env     map[string]string
		want    ImageMode
	}{
		{"explicit setting wins", "blocks", map[string]string{"TERM": "xterm-kitty"}, ImagesBlocks},
		{"explicit off", "off", map[string]string{"TERM": "xterm-kitty"}, ImagesOff},
		{"kitty by TERM", "auto", map[string]string{"TERM": "xterm-kitty"}, ImagesKitty},
		{"kitty by window id (container TERM is plain)", "auto", map[string]string{"TERM": "xterm", "KITTY_WINDOW_ID": "1"}, ImagesKitty},
		{"ghostty", "auto", map[string]string{"TERM_PROGRAM": "ghostty"}, ImagesKitty},
		{"tmux falls back to blocks", "auto", map[string]string{"TMUX": "/tmp/tmux", "TERM": "xterm-kitty", "COLORTERM": "truecolor"}, ImagesBlocks},
		{"truecolor terminal", "auto", map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}, ImagesBlocks},
		{"no truecolor means no images", "auto", map[string]string{"TERM": "xterm"}, ImagesOff},
		{"garbage setting is auto", "sixel", map[string]string{"TERM": "xterm-kitty"}, ImagesKitty},
	}
	for _, tc := range cases {
		if got := DetectImageMode(tc.setting, envOf(tc.env)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPreviewSizeKeepsAspectAndFitsTheBox(t *testing.T) {
	cols, rows := previewSize(640, 640)
	if cols != previewMaxCols || rows != 22 {
		t.Fatalf("square image should be %dx22 cells, got %dx%d", previewMaxCols, cols, rows)
	}
	cols, rows = previewSize(1000, 100)
	if cols != previewMaxCols || rows != 2 {
		t.Fatalf("wide image should keep full width, got %dx%d", cols, rows)
	}
	cols, rows = previewSize(100, 2000)
	if rows != previewMaxRows || cols >= previewMaxCols {
		t.Fatalf("tall image should be limited by height, got %dx%d", cols, rows)
	}
}

func TestKittyTransmitChunksTheImageAndSilencesReplies(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 1500) // ~20 KB base64
	out := string(kittyTransmit(1234, data, 30, 12))

	sequences := regexp.MustCompile("\x1b_G([^;\x1b]*)(?:;([^\x1b]*))?\x1b\\\\").FindAllStringSubmatch(out, -1)
	if len(sequences) < 4 {
		t.Fatalf("expected several chunks plus a placement, got %d in %q", len(sequences), out[:80])
	}
	first := sequences[0][1]
	for _, want := range []string{"a=t", "f=100", "t=d", "i=1234", "q=2", "m=1"} {
		if !strings.Contains(first, want) {
			t.Errorf("first chunk %q is missing %q", first, want)
		}
	}

	var payload strings.Builder
	chunks := sequences[:len(sequences)-1]
	for i, seq := range chunks {
		if len(seq[2]) > 4096 {
			t.Errorf("chunk %d is %d bytes, over the 4096 limit", i, len(seq[2]))
		}
		wantMore := "m=1"
		if i == len(chunks)-1 {
			wantMore = "m=0"
		}
		if !strings.Contains(seq[1], wantMore) || !strings.Contains(seq[1], "q=2") {
			t.Errorf("chunk %d keys %q should contain %s and q=2", i, seq[1], wantMore)
		}
		payload.WriteString(seq[2])
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatalf("payload does not round-trip: %v", err)
	}

	placement := sequences[len(sequences)-1][1]
	for _, want := range []string{"a=p", "U=1", "i=1234", "c=30", "r=12", "q=2"} {
		if !strings.Contains(placement, want) {
			t.Errorf("placement %q is missing %q", placement, want)
		}
	}
}

func TestKittyPlaceholdersEncodeRowColumnAndImageID(t *testing.T) {
	id := uint32(0x010203)
	text := kittyPlaceholders(id, 5, 3)

	lines := strings.Split(text, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(lines))
	}
	for row, line := range lines {
		if !strings.HasPrefix(line, "\x1b[38;2;1;2;3m") || !strings.HasSuffix(line, "\x1b[39m") {
			t.Errorf("row %d must carry the image id as its foreground color: %q", row, line)
		}
		cells := []rune(strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[38;2;1;2;3m"), "\x1b[39m"))
		if len(cells) != 5*3 {
			t.Fatalf("row %d: expected 5 cells of 3 runes, got %d runes", row, len(cells))
		}
		for col := 0; col < 5; col++ {
			cell := cells[col*3 : col*3+3]
			if cell[0] != 0x10EEEE || cell[1] != kittyDiacritics[row] || cell[2] != kittyDiacritics[col] {
				t.Errorf("cell (%d,%d) is %U, want placeholder + row + column marks", row, col, cell)
			}
		}
	}
}

func TestKittyPlaceholdersNeverExceedTheDiacriticTable(t *testing.T) {
	text := kittyPlaceholders(1000, 500, 500)
	if rows := strings.Count(text, "\n") + 1; rows != len(kittyDiacritics) {
		t.Fatalf("rows should be capped at %d, got %d", len(kittyDiacritics), rows)
	}
	if previewMaxCols > len(kittyDiacritics) || previewMaxRows > len(kittyDiacritics) {
		t.Fatal("the preview box must fit in the diacritics table")
	}
}

func TestBuildPreviewKittyUploadsAndReturnsPlaceholders(t *testing.T) {
	p, transmit, err := buildPreview(ImagesKitty, "https://x/a.png", testPNG(t, 120, 80), 1500)
	if err != nil {
		t.Fatal(err)
	}
	if p.id != 1500 || !strings.Contains(string(transmit), "i=1500") || !strings.Contains(p.text, "\U0010EEEE") {
		t.Fatalf("unexpected kitty preview %+v", p)
	}
	if strings.Count(p.text, "\n")+1 != p.rows {
		t.Fatalf("placeholder rows %d do not match preview rows %d", strings.Count(p.text, "\n")+1, p.rows)
	}
}

func TestBuildPreviewBlocksHasTwoPixelRowsPerTextRow(t *testing.T) {
	p, transmit, err := buildPreview(ImagesBlocks, "https://x/a.png", testPNG(t, 200, 200), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(transmit) != 0 || p.id != 0 {
		t.Fatal("blocks mode must not talk to the terminal")
	}
	lines := strings.Split(p.text, "\n")
	if len(lines) != p.rows {
		t.Fatalf("expected %d text rows, got %d", p.rows, len(lines))
	}
	if cells := strings.Count(lines[0], "▀"); cells != p.cols {
		t.Fatalf("expected %d cells per row, got %d", p.cols, cells)
	}
}

func TestBuildPreviewRejectsNonImages(t *testing.T) {
	if _, _, err := buildPreview(ImagesKitty, "https://x/v.mp4", []byte("not an image"), 1000); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestSafeOutputWritesThroughToTheFile(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "tty"))
	if err != nil {
		t.Fatal(err)
	}
	out := NewSafeOutput(file)
	if _, err := out.Write([]byte("frame")); err != nil {
		t.Fatal(err)
	}
	if err := out.WriteRaw([]byte("|escape")); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(file.Name())
	if string(content) != "frame|escape" {
		t.Fatalf("unexpected output %q", content)
	}
}

type fakeSink struct{ writes []string }

func (f *fakeSink) WriteRaw(p []byte) error {
	f.writes = append(f.writes, string(p))
	return nil
}

func (f *fakeSink) joined() string { return strings.Join(f.writes, "") }

type fakeFetcher struct {
	t     *testing.T
	calls []string
	fail  map[string]error
}

func (f *fakeFetcher) fetch(_ context.Context, url string) ([]byte, error) {
	f.calls = append(f.calls, url)
	if err := f.fail[url]; err != nil {
		return nil, err
	}
	return testPNG(f.t, 90, 60), nil
}

func withImages(t *testing.T, mode ImageMode) (*Model, *fakeClient, *fakeSink, *fakeFetcher) {
	t.Helper()
	fake := newFake()
	m := NewModel(fake, 1, 1000000000)
	sink, fetcher := &fakeSink{}, &fakeFetcher{t: t, fail: map[string]error{}}
	m.EnableImages(mode, sink, fetcher.fetch)
	m.width = 140
	pump(m, m.Init())
	return m, fake, sink, fetcher
}

func TestKittyPreviewIsFetchedOncePerMemeAndUploadedToTheTerminal(t *testing.T) {
	m, _, sink, fetcher := withImages(t, ImagesKitty)
	press(m, "2")

	if len(fetcher.calls) != 1 || fetcher.calls[0] != "https://x/a.jpeg" {
		t.Fatalf("expected the selected meme to be fetched, got %v", fetcher.calls)
	}
	if !strings.Contains(sink.joined(), "a=t") || !strings.Contains(sink.joined(), "i=1000") {
		t.Fatalf("expected an upload for image 1000, got %q", sink.joined())
	}
	if !strings.Contains(m.View(), "\U0010EEEE") {
		t.Fatalf("view should contain the image placeholders:\n%s", m.View())
	}

	press(m, "down")
	press(m, "up")
	if len(fetcher.calls) != 2 {
		t.Fatalf("second meme fetched once, first served from cache; got %v", fetcher.calls)
	}
	if strings.Count(sink.joined(), "a=t") != 2 {
		t.Fatalf("each image must be uploaded once, got %d uploads", strings.Count(sink.joined(), "a=t"))
	}
}

func TestBlocksPreviewNeedsNoTerminalCooperation(t *testing.T) {
	m, _, sink, _ := withImages(t, ImagesBlocks)
	press(m, "2")

	if len(sink.writes) != 0 {
		t.Fatalf("blocks mode must not write raw sequences, got %q", sink.joined())
	}
	if !strings.Contains(m.View(), "▀") {
		t.Fatalf("view should contain half blocks:\n%s", m.View())
	}
}

func TestPreviewFailuresAndVideosDegradeToAMessage(t *testing.T) {
	m, _, _, fetcher := withImages(t, ImagesKitty)
	fetcher.fail["https://x/a.jpeg"] = errors.New("download returned 403")
	press(m, "2")
	if !strings.Contains(m.View(), "sem prévia") || !strings.Contains(m.View(), "403") {
		t.Fatalf("expected a no-preview message:\n%s", m.View())
	}

	m2, fake, _, fetcher2 := withImages(t, ImagesKitty)
	fake.responses["meme.list"]["memes"] = []any{map[string]any{"url": "https://x/clip.mp4", "title": "video"}}
	press(m2, "2")
	if len(fetcher2.calls) != 0 || !strings.Contains(m2.View(), "vídeo") {
		t.Fatalf("videos must not be downloaded, calls=%v:\n%s", fetcher2.calls, m2.View())
	}
}

func TestImagesOffNeverFetches(t *testing.T) {
	m, _, sink, fetcher := withImages(t, ImagesOff)
	press(m, "2", "down")
	if len(fetcher.calls) != 0 || len(sink.writes) != 0 {
		t.Fatal("previews are off")
	}
}

func TestOldPreviewsAreEvictedAndDeletedFromTheTerminal(t *testing.T) {
	m, fake, sink, _ := withImages(t, ImagesKitty)
	items := []any{}
	for i := 0; i < previewCacheSize+3; i++ {
		items = append(items, map[string]any{"url": fmt.Sprintf("https://x/%d.jpeg", i), "title": "m"})
	}
	fake.responses["meme.list"]["memes"] = items
	press(m, "2")
	for i := 0; i < previewCacheSize+2; i++ {
		press(m, "down")
	}

	if len(m.images.cache) != previewCacheSize {
		t.Fatalf("cache should be capped at %d, has %d", previewCacheSize, len(m.images.cache))
	}
	if !strings.Contains(sink.joined(), "a=d,d=I,i=1000") {
		t.Fatalf("the oldest image must be deleted from the terminal, got deletes: %v", regexp.MustCompile(`a=d[^\x1b]*`).FindAllString(sink.joined(), -1))
	}
	if len(m.ImageIDs()) != previewCacheSize {
		t.Fatalf("ImageIDs should report the live images, got %d", len(m.ImageIDs()))
	}
}

func TestNarrowTerminalsPlaceThePreviewBelowTheList(t *testing.T) {
	m, _, _, _ := withImages(t, ImagesBlocks)
	m.width = 80
	press(m, "2")

	view := m.View()
	listLine := strings.Index(view, "Primeiro meme")
	image := strings.Index(view, "▀")
	detail := strings.Index(view, "Selecionado")
	if listLine < 0 || image < 0 || detail < 0 || !(listLine < detail && detail < image) {
		t.Fatalf("narrow layout should be list, detail, then image (list=%d detail=%d image=%d)", listLine, detail, image)
	}
}
