package tui

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ImageMode is how meme previews are drawn in the terminal.
type ImageMode string

const (
	ImagesOff    ImageMode = "off"
	ImagesBlocks ImageMode = "blocks" // half-block characters; any truecolor terminal
	ImagesKitty  ImageMode = "kitty"  // kitty graphics protocol with unicode placeholders
)

const (
	previewMaxCols   = 44
	previewMaxRows   = 22
	kittyMaxPixels   = 640
	maxImageBytes    = 8 << 20
	previewCacheSize = 24
	firstKittyID     = 1000

	videoMaxFrames   = 40
	videoFPS         = 8
	videoMaxSeconds  = 5
	videoFrameWidth  = 360
	maxVideoBytes    = 24 << 20
	maxCachedFrames  = 240
	frameInterval    = time.Second / videoFPS
	browserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0 Safari/537.36"
	// A terminal cell is about twice as tall as it is wide.
	cellAspect = 0.5
)

// DetectImageMode picks a mode from the user's setting ("auto" looks at the terminal).
func DetectImageMode(setting string, env func(string) string) ImageMode {
	switch mode := ImageMode(strings.ToLower(strings.TrimSpace(setting))); mode {
	case ImagesOff, ImagesBlocks, ImagesKitty:
		return mode
	}
	// tmux swallows graphics escapes unless passthrough is configured.
	if env("TMUX") != "" {
		return truecolorOr(env, ImagesBlocks)
	}
	if strings.Contains(env("TERM"), "kitty") || env("KITTY_WINDOW_ID") != "" ||
		env("TERM_PROGRAM") == "ghostty" || env("GHOSTTY_RESOURCES_DIR") != "" {
		return ImagesKitty
	}
	return truecolorOr(env, ImagesBlocks)
}

func truecolorOr(env func(string) string, mode ImageMode) ImageMode {
	if c := env("COLORTERM"); c == "truecolor" || c == "24bit" {
		return mode
	}
	return ImagesOff
}

// TerminalSink receives raw escape sequences that must not be mixed into a frame.
type TerminalSink interface {
	WriteRaw(p []byte) error
}

// ImageFetcher downloads an image.
type ImageFetcher func(ctx context.Context, url string) ([]byte, error)

// originOf returns "scheme://host/" for a URL, or "" when it has no host.
func originOf(rawURL string) string {
	parsed, err := neturl.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

func HTTPFetcher(ctx context.Context, url string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Image hosts often refuse hotlinked media that does not look like a browser on their
	// own site, so present a browser User-Agent and the host's own origin as the Referer.
	request.Header.Set("User-Agent", browserUserAgent)
	if origin := originOf(url); origin != "" {
		request.Header.Set("Referer", origin)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %d", response.StatusCode)
	}
	limit := int64(maxImageBytes)
	if isVideo(url) {
		limit = maxVideoBytes
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file is larger than %d MB", limit>>20)
	}
	return data, nil
}

// SafeOutput serializes everything written to the terminal. Bubble Tea's renderer
// and the image transmissions share it, so an escape sequence is never split by a frame.
type SafeOutput struct {
	*os.File
	mu sync.Mutex
}

func NewSafeOutput(file *os.File) *SafeOutput { return &SafeOutput{File: file} }

func (o *SafeOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.File.Write(p)
}

func (o *SafeOutput) WriteRaw(p []byte) error {
	_, err := o.Write(p)
	return err
}

// frame is one picture of a preview: the text that draws it and, in kitty mode, the
// id of the image the placeholders point at.
type frame struct {
	id   uint32
	text string
}

// preview is a meme image, or a short looping video, ready to be drawn.
type preview struct {
	url        string
	id         uint32 // kitty id of a still image; 0 in blocks mode
	cols, rows int
	text       string
	frames     []frame // more than one entry means a video
	err        string

	// transmit holds the kitty upload until this preview is actually on screen, so
	// scrolling past a video does not push megabytes into the terminal.
	transmit []byte
	uploaded bool
}

func (p *preview) animated() bool { return len(p.frames) > 1 }

func (p *preview) textAt(index int) string {
	if p.animated() {
		return p.frames[index%len(p.frames)].text
	}
	return p.text
}

func (p *preview) weight() int { return max(len(p.frames), 1) }

func (p *preview) kittyIDs() []uint32 {
	if p.animated() {
		ids := make([]uint32, 0, len(p.frames))
		for _, f := range p.frames {
			if f.id != 0 {
				ids = append(ids, f.id)
			}
		}
		return ids
	}
	if p.id != 0 {
		return []uint32{p.id}
	}
	return nil
}

// buildPreview decodes data and returns the preview plus, in kitty mode, the escape
// sequences that upload it to the terminal.
func buildPreview(mode ImageMode, url string, data []byte, id uint32) (*preview, []byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("not a supported image: %w", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		return nil, nil, fmt.Errorf("empty image")
	}
	cols, rows := previewSize(bounds.Dx(), bounds.Dy())
	result := &preview{url: url, cols: cols, rows: rows}

	if mode == ImagesKitty {
		encoded, err := encodePNG(scaleToFit(img, kittyMaxPixels, kittyMaxPixels))
		if err != nil {
			return nil, nil, err
		}
		result.id = id
		result.text = kittyPlaceholders(id, cols, rows)
		return result, kittyTransmit(id, encoded, cols, rows), nil
	}
	result.text = halfBlocks(scaleExact(img, cols, rows*2))
	return result, nil, nil
}

// previewSize fits an image in previewMaxCols x previewMaxRows cells, keeping its aspect.
func previewSize(width, height int) (cols, rows int) {
	ratio := float64(height) / float64(width) * cellAspect
	cols = previewMaxCols
	rows = int(float64(cols)*ratio + 0.5)
	if rows > previewMaxRows {
		rows = previewMaxRows
		cols = int(float64(rows)/ratio + 0.5)
	}
	return max(cols, 1), max(rows, 1)
}

func scaleToFit(src image.Image, maxW, maxH int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxW && h <= maxH {
		return src
	}
	scale := min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	return scaleExact(src, max(int(float64(w)*scale), 1), max(int(float64(h)*scale), 1))
}

func scaleExact(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func encodePNG(img image.Image) ([]byte, error) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// halfBlocks draws two pixel rows per text row: the upper one as foreground of "▀"
// and the lower one as its background.
func halfBlocks(img image.Image) string {
	b := img.Bounds()
	var out strings.Builder
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x++ {
			tr, tg, tb, _ := img.At(x, y).RGBA()
			br, bg, bb := tr, tg, tb
			if y+1 < b.Max.Y {
				br, bg, bb, _ = img.At(x, y+1).RGBA()
			}
			fmt.Fprintf(&out, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", tr>>8, tg>>8, tb>>8, br>>8, bg>>8, bb>>8)
		}
		out.WriteString("\x1b[0m")
		if y+2 < b.Max.Y {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// kittyTransmit uploads a PNG (kitty graphics protocol, direct transmission in 4 KB
// chunks) and creates a virtual placement sized cols x rows. q=2 silences the
// terminal's replies, which would otherwise arrive on stdin as bogus keystrokes.
func kittyTransmit(id uint32, pngData []byte, cols, rows int) []byte {
	encoded := base64.StdEncoding.EncodeToString(pngData)
	var out bytes.Buffer
	for first := true; len(encoded) > 0; first = false {
		n := min(4096, len(encoded))
		chunk := encoded[:n]
		encoded = encoded[n:]
		more := 0
		if len(encoded) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&out, "\x1b_Ga=t,f=100,t=d,i=%d,q=2,m=%d;%s\x1b\\", id, more, chunk)
		} else {
			fmt.Fprintf(&out, "\x1b_Gm=%d,q=2;%s\x1b\\", more, chunk)
		}
	}
	fmt.Fprintf(&out, "\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows)
	return out.Bytes()
}

func kittyDelete(id uint32) []byte {
	return []byte(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id))
}

// kittyPlaceholders returns the text that stands for the image: one U+10EEEE cell per
// image cell, its row and column given as combining marks and the image id as the
// foreground color. Being plain text, it survives Bubble Tea's redraws and layout.
func kittyPlaceholders(id uint32, cols, rows int) string {
	cols = min(cols, len(kittyDiacritics))
	rows = min(rows, len(kittyDiacritics))
	style := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xff, id>>8&0xff, id&0xff)
	var out strings.Builder
	for row := 0; row < rows; row++ {
		out.WriteString(style)
		for col := 0; col < cols; col++ {
			out.WriteRune(0x10EEEE)
			out.WriteRune(kittyDiacritics[row])
			out.WriteRune(kittyDiacritics[col])
		}
		out.WriteString("\x1b[39m")
		if row < rows-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// VideoFramer turns a video file into PNG frames (a short, low-rate excerpt).
type VideoFramer func(ctx context.Context, data []byte) ([][]byte, error)

// FFmpegFrames extracts up to videoMaxSeconds of the video at videoFPS with ffmpeg.
func FFmpegFrames(ctx context.Context, data []byte) ([][]byte, error) {
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New(tr("ffmpeg not found: no video preview"))
	}
	// mp4 files often keep their index at the end, so ffmpeg needs a seekable file.
	file, err := os.CreateTemp("", "dokja-video-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return nil, err
	}
	file.Close()

	command := exec.CommandContext(ctx, binary, "-v", "error", "-i", file.Name(),
		"-t", fmt.Sprint(videoMaxSeconds),
		"-vf", fmt.Sprintf("fps=%d,scale=%d:-2:flags=lanczos", videoFPS, videoFrameWidth),
		"-frames:v", fmt.Sprint(videoMaxFrames), "-f", "image2pipe", "-c:v", "png", "pipe:1")
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, fmt.Errorf("ffmpeg: %w", err)
	}
	frames := splitPNGs(output)
	if len(frames) == 0 {
		return nil, errors.New(tr("ffmpeg produced no frames"))
	}
	return frames, nil
}

// splitPNGs cuts a stream of concatenated PNG files into the individual files.
func splitPNGs(stream []byte) [][]byte {
	signature := []byte("\x89PNG\r\n\x1a\n")
	var frames [][]byte
	for len(stream) >= len(signature) && bytes.HasPrefix(stream, signature) {
		offset := len(signature)
		for offset+12 <= len(stream) {
			length := int(binary.BigEndian.Uint32(stream[offset : offset+4]))
			chunkType := string(stream[offset+4 : offset+8])
			offset += 12 + length
			if chunkType == "IEND" {
				break
			}
		}
		if offset > len(stream) {
			break
		}
		frames = append(frames, stream[:offset])
		stream = stream[offset:]
	}
	return frames
}

// buildVideoPreview draws each PNG frame; in kitty mode it uploads them under
// consecutive ids starting at firstID (one placement each).
func buildVideoPreview(mode ImageMode, url string, pngs [][]byte, firstID uint32) (*preview, []byte, error) {
	var transmit []byte
	result := &preview{url: url}
	for index, data := range pngs {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, nil, fmt.Errorf("frame %d: %w", index, err)
		}
		if index == 0 {
			result.cols, result.rows = previewSize(img.Bounds().Dx(), img.Bounds().Dy())
		}
		if mode == ImagesKitty {
			id := firstID + uint32(index)
			transmit = append(transmit, kittyTransmit(id, data, result.cols, result.rows)...)
			result.frames = append(result.frames, frame{id: id, text: kittyPlaceholders(id, result.cols, result.rows)})
		} else {
			result.frames = append(result.frames, frame{text: halfBlocks(scaleExact(img, result.cols, result.rows*2))})
		}
	}
	if len(result.frames) == 0 {
		return nil, nil, errors.New("video has no frames")
	}
	return result, transmit, nil
}

type imageMsg struct {
	url      string
	preview  *preview
	transmit []byte
	err      error
}

// imageState caches previews (most recent last) and remembers what is being fetched.
type imageState struct {
	mode     ImageMode
	sink     TerminalSink
	fetch    ImageFetcher
	cache    map[string]*preview
	order    []string
	inflight map[string]bool
	nextID   uint32

	framer  VideoFramer
	frame   int // frame of the selected video being shown
	animGen int // bumped whenever the selection changes, to drop stale ticks
}

func newImageState() imageState {
	return imageState{mode: ImagesOff, cache: map[string]*preview{}, inflight: map[string]bool{}, nextID: firstKittyID}
}

func (s *imageState) enabled() bool { return s.mode != ImagesOff && s.fetch != nil }

func (s *imageState) remember(p *preview) []byte {
	var cleanup []byte
	s.cache[p.url] = p
	s.order = append(s.order, p.url)
	for len(s.order) > 1 && (len(s.order) > previewCacheSize || s.cachedFrames() > maxCachedFrames) {
		oldest := s.order[0]
		s.order = s.order[1:]
		if evicted := s.cache[oldest]; evicted != nil {
			for _, id := range evicted.kittyIDs() {
				cleanup = append(cleanup, kittyDelete(id)...)
			}
		}
		delete(s.cache, oldest)
	}
	return cleanup
}

func (s *imageState) cachedFrames() int {
	total := 0
	for _, p := range s.cache {
		total += p.weight()
	}
	return total
}

// ids lists the kitty images this session uploaded, so they can be deleted on exit.
func (s *imageState) ids() []uint32 {
	var ids []uint32
	for _, p := range s.cache {
		ids = append(ids, p.kittyIDs()...)
	}
	return ids
}
