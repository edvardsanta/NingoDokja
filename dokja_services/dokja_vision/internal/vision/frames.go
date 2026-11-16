// Frame sampling for videos a caller hands over as bytes. ffmpeg does the decoding;
// the model receives a bounded set of JPEG frames from across the whole video, so a
// reel is read by its scenes rather than by its URL or login wall.
package vision

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// maxVideoFrames bounds how many frames of one video a reading carries.
const maxVideoFrames = 6

// maxVideoBytes bounds a video handed over as bytes for one reading.
const maxVideoBytes = 16 << 20

// extractFrames samples a video into base64 JPEG frames. It is a variable so tests can
// stand in a fake; production uses extractFramesWithFFmpeg.
var extractFrames = extractFramesWithFFmpeg

// extractFramesWithFFmpeg spreads maxVideoFrames frames across the video's duration,
// scales them down to what the vision model needs and encodes JPEG. ffmpeg and ffprobe
// come from the service image; their absence is an operator problem, reported plainly.
func extractFramesWithFFmpeg(ctx context.Context, data []byte) ([]string, error) {
	if len(data) > maxVideoBytes {
		return nil, fmt.Errorf("%w: video exceeds the size limit", ErrTooLarge)
	}
	duration := 0.0
	if probed, err := probeDuration(ctx, data); err == nil {
		duration = probed
	}
	fps := 0.5
	if duration > 0 {
		fps = float64(maxVideoFrames) / duration
	}
	if fps > 4 {
		fps = 4
	}
	dir, err := os.MkdirTemp("", "dokja-vision-frames-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	command := exec.CommandContext(ctx, "ffmpeg", "-v", "error",
		"-i", "pipe:0",
		"-vf", fmt.Sprintf("fps=%.4f,scale='min(768,iw)':-2", fps),
		"-frames:v", strconv.Itoa(maxVideoFrames),
		"-f", "image2", filepath.Join(dir, "frame-%02d.jpg"))
	command.Stdin = bytes.NewReader(data)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%w: video decode failed: %v: %s", ErrFetchFailed, err, strings.TrimSpace(string(output)))
	}
	files, err := filepath.Glob(filepath.Join(dir, "frame-*.jpg"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("%w: no video frames decoded", ErrFetchFailed)
	}
	frames := make([]string, 0, len(files))
	for _, file := range files {
		jpeg, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFetchFailed, err)
		}
		frames = append(frames, base64.StdEncoding.EncodeToString(jpeg))
	}
	return frames, nil
}

// probeDuration reads a video's seconds with ffprobe; a missing answer just means the
// fallback sampling interval is used.
func probeDuration(ctx context.Context, data []byte) (float64, error) {
	command := exec.CommandContext(ctx, "ffprobe", "-v", "error",
		"-show_entries", "format=duration", "-of", "csv=p=0", "-i", "pipe:0")
	command.Stdin = bytes.NewReader(data)
	output, err := command.Output()
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("no duration")
	}
	return seconds, nil
}
