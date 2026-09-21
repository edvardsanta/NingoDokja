package tui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func pngs(t *testing.T, n int) [][]byte {
	t.Helper()
	frames := make([][]byte, n)
	for i := range frames {
		frames[i] = testPNG(t, 64+i, 48)
	}
	return frames
}

func TestSplitPNGsCutsAConcatenatedStream(t *testing.T) {
	frames := pngs(t, 3)
	stream := bytes.Join(frames, nil)

	got := splitPNGs(stream)
	if len(got) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(got))
	}
	for i, frame := range got {
		if !bytes.Equal(frame, frames[i]) {
			t.Errorf("frame %d was altered", i)
		}
		if _, _, err := image.Decode(bytes.NewReader(frame)); err != nil {
			t.Errorf("frame %d does not decode: %v", i, err)
		}
	}
	if len(splitPNGs([]byte("garbage"))) != 0 || len(splitPNGs(nil)) != 0 {
		t.Fatal("garbage must yield no frames")
	}
	if got := splitPNGs(stream[:len(stream)-20]); len(got) != 2 {
		t.Fatalf("a truncated last frame must be dropped, got %d frames", len(got))
	}
}

func TestBuildVideoPreviewUploadsEachFrameUnderItsOwnID(t *testing.T) {
	p, transmit, err := buildVideoPreview(ImagesKitty, "https://x/v.mp4", pngs(t, 3), 2000)
	if err != nil {
		t.Fatal(err)
	}
	if !p.animated() || len(p.frames) != 3 {
		t.Fatalf("expected an animated preview with 3 frames, got %+v", p)
	}
	for i, want := range []uint32{2000, 2001, 2002} {
		if p.frames[i].id != want {
			t.Errorf("frame %d id = %d, want %d", i, p.frames[i].id, want)
		}
		if strings.Count(string(transmit), fmt.Sprintf("a=t,f=100,t=d,i=%d,", want)) != 1 ||
			strings.Count(string(transmit), fmt.Sprintf("a=p,U=1,i=%d,", want)) != 1 {
			t.Errorf("frame id %d must be uploaded and placed exactly once", want)
		}
	}
	if p.frames[0].text == p.frames[1].text {
		t.Fatal("frames must differ (each carries its own image id)")
	}
	if got := p.kittyIDs(); len(got) != 3 || got[0] != 2000 || got[2] != 2002 {
		t.Fatalf("kittyIDs = %v", got)
	}
}

func TestBuildVideoPreviewBlocksNeedsNoUpload(t *testing.T) {
	p, transmit, err := buildVideoPreview(ImagesBlocks, "https://x/v.mp4", pngs(t, 2), 0)
	if err != nil || len(transmit) != 0 || len(p.frames) != 2 || !strings.Contains(p.frames[0].text, "▀") {
		t.Fatalf("unexpected blocks video preview: %v %d %+v", err, len(transmit), p)
	}
	if _, _, err := buildVideoPreview(ImagesBlocks, "u", nil, 0); err == nil {
		t.Fatal("no frames must be an error")
	}
}

func videoModel(t *testing.T, mode ImageMode) (*Model, *fakeSink, *int) {
	t.Helper()
	m, fake, sink, _ := withImages(t, mode)
	framed := 0
	m.SetVideoFramer(func(context.Context, []byte) ([][]byte, error) {
		framed++
		return pngs(t, 3), nil
	})
	fake.responses["meme.list"]["memes"] = []any{
		map[string]any{"url": "https://x/clip.mp4", "title": "video"},
		map[string]any{"url": "https://x/still.jpeg", "title": "foto"},
	}
	return m, sink, &framed
}

func firstColorID(view string) string {
	match := regexp.MustCompile(`\x1b\[38;2;(\d+;\d+;\d+)m\x{10EEEE}`).FindStringSubmatch(view)
	if match == nil {
		return ""
	}
	return match[1]
}

func TestVideoPlaysByAdvancingFramesOnTicks(t *testing.T) {
	m, sink, framed := videoModel(t, ImagesKitty)
	press(m, "2")

	if *framed != 1 || strings.Count(sink.joined(), "a=t") != 3 {
		t.Fatalf("video framed once and its 3 frames uploaded once: framed=%d uploads=%d", *framed, strings.Count(sink.joined(), "a=t"))
	}
	first := firstColorID(m.View())
	if first == "" {
		t.Fatalf("expected the first frame on screen:\n%s", m.View())
	}

	gen := m.images.animGen
	_, cmd := m.Update(frameTickMsg{url: "https://x/clip.mp4", gen: gen})
	if cmd == nil {
		t.Fatal("the loop must re-arm itself")
	}
	second := firstColorID(m.View())
	if second == "" || second == first {
		t.Fatalf("the frame should have advanced (%q -> %q)", first, second)
	}

	m.Update(frameTickMsg{url: "https://x/clip.mp4", gen: gen})
	m.Update(frameTickMsg{url: "https://x/clip.mp4", gen: gen})
	if firstColorID(m.View()) != first {
		t.Fatal("playback should loop back to the first frame after 3 frames")
	}
}

func TestStaleAndForeignTicksAreIgnored(t *testing.T) {
	m, _, _ := videoModel(t, ImagesKitty)
	press(m, "2")
	gen, before := m.images.animGen, m.images.frame

	for name, tick := range map[string]frameTickMsg{
		"old generation": {url: "https://x/clip.mp4", gen: gen - 1},
		"other meme":     {url: "https://x/still.jpeg", gen: gen},
	} {
		if _, cmd := m.Update(tick); cmd != nil || m.images.frame != before {
			t.Errorf("%s: tick must be dropped without re-arming", name)
		}
	}
}

func TestPlaybackStopsOffTheMemesTabAndResumesOnReturn(t *testing.T) {
	m, _, _ := videoModel(t, ImagesKitty)
	press(m, "2")
	gen := m.images.animGen

	press(m, "1") // Painel
	if _, cmd := m.Update(frameTickMsg{url: "https://x/clip.mp4", gen: gen}); cmd != nil {
		t.Fatal("no ticks off the Memes tab")
	}

	_, cmd := m.Update(key("2"))
	if cmd == nil {
		t.Fatal("returning to Memes must restart playback of the cached video")
	}
	if m.images.frame != 0 {
		t.Fatal("playback restarts from the first frame")
	}
}

func TestMovingToAStillStopsTheLoop(t *testing.T) {
	m, _, _ := videoModel(t, ImagesKitty)
	press(m, "2")
	gen := m.images.animGen

	press(m, "down")
	if _, cmd := m.Update(frameTickMsg{url: "https://x/clip.mp4", gen: gen}); cmd != nil {
		t.Fatal("the video's ticks must die once another meme is selected")
	}
}

func TestVideosAreCachedByFrameBudgetAndFullyDeleted(t *testing.T) {
	m, fake, sink, _ := withImages(t, ImagesKitty)
	m.SetVideoFramer(func(context.Context, []byte) ([][]byte, error) { return pngs(t, videoMaxFrames), nil })
	items := []any{}
	for i := 0; i < 8; i++ {
		items = append(items, map[string]any{"url": fmt.Sprintf("https://x/%d.mp4", i), "title": "v"})
	}
	fake.responses["meme.list"]["memes"] = items
	press(m, "2")
	for i := 0; i < 7; i++ {
		press(m, "down")
	}

	if got := m.images.cachedFrames(); got > maxCachedFrames {
		t.Fatalf("cached frames %d exceed the budget %d", got, maxCachedFrames)
	}
	if deletes := strings.Count(sink.joined(), "a=d,d=I"); deletes < videoMaxFrames {
		t.Fatalf("evicting a video must delete all its frames, saw %d deletes", deletes)
	}
}

func TestFFmpegFramesWithARealVideo(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	path := filepath.Join(t.TempDir(), "test.mp4")
	command := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=10",
		"-pix_fmt", "yuv420p", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("cannot build a sample video: %v %s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	frames, err := FFmpegFrames(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) < 6 || len(frames) > videoMaxFrames {
		t.Fatalf("a 1 second clip at %d fps should give ~%d frames, got %d", videoFPS, videoFPS, len(frames))
	}
	img, _, err := image.Decode(bytes.NewReader(frames[0]))
	if err != nil || img.Bounds().Dx() != videoFrameWidth {
		t.Fatalf("frames should be scaled to %d px wide: %v %v", videoFrameWidth, err, img)
	}

	if _, err := FFmpegFrames(context.Background(), []byte("not a video")); err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("garbage must fail with an ffmpeg error, got %v", err)
	}
}

func TestHTTPFetcherSendsBrowserHeadersAndTheHostsOwnOriginAsReferer(t *testing.T) {
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		if strings.HasSuffix(r.URL.Path, "/forbidden.png") {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		w.Write([]byte("bytes"))
	}))
	defer server.Close()

	if _, err := HTTPFetcher(context.Background(), server.URL+"/deep/path/a.png"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen.Get("User-Agent"), "Mozilla") || seen.Get("Referer") != server.URL+"/" {
		t.Fatalf("downloads need a browser UA and the host's own origin as Referer, got %v", seen)
	}

	if _, err := HTTPFetcher(context.Background(), server.URL+"/x/forbidden.png"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected the 403 to surface, got %v", err)
	}
}

func TestPreviewsAreOnlyUploadedToTheTerminalWhenSelected(t *testing.T) {
	m, fake, sink, _ := withImages(t, ImagesKitty)
	fake.responses["meme.list"]["memes"] = []any{
		map[string]any{"url": "https://x/a.jpeg", "title": "a"},
		map[string]any{"url": "https://x/b.jpeg", "title": "b"},
	}
	press(m, "2")
	uploaded := strings.Count(sink.joined(), "a=t")

	built, transmit, err := buildPreview(ImagesKitty, "https://x/b.jpeg", testPNG(t, 60, 40), 1500)
	if err != nil {
		t.Fatal(err)
	}
	m.Update(imageMsg{url: "https://x/b.jpeg", preview: built, transmit: transmit})
	if strings.Count(sink.joined(), "a=t") != uploaded || strings.Contains(sink.joined(), "i=1500") {
		t.Fatal("a preview that is not selected must not be uploaded")
	}

	press(m, "down")
	if !strings.Contains(sink.joined(), "i=1500") {
		t.Fatal("selecting the meme must upload its preview")
	}
	press(m, "up", "down")
	if n := strings.Count(sink.joined(), "a=t,f=100,t=d,i=1500,"); n != 1 {
		t.Fatalf("a preview must be uploaded once, got %d uploads", n)
	}
}
