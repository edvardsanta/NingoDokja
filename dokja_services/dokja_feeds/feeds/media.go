//go:build unix

package feeds

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

const (
	// ActionMediaFile asks a post reader to write one file to a path the host chose.
	ActionMediaFile = "media_file"

	// MaxMediaFileBytes is the largest post file the host keeps: WhatsApp's video limit.
	MaxMediaFileBytes = 100_000_000
	// MediaChunkBytes is the most one feeds.media.read hands over, so no reply holds a whole file.
	MediaChunkBytes = 1 << 20

	mediaTTL      = 15 * time.Minute
	maxSpoolBytes = 300_000_000
	refusedTooBig = "media_too_large"
)

var (
	mediaToken = regexp.MustCompile(`^[a-f0-9]{48}$`)
	mediaTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "video/mp4": true}
)

type spooledMedia struct {
	path        string
	contentType string
	size        int64
	expires     time.Time
}

// mediaSpool holds files for a short while between a reader writing them and a caller pulling
// them. Callers only ever hold a random token; a path never leaves this process and its plugins.
type mediaSpool struct {
	mu     sync.Mutex
	now    func() time.Time
	dir    string
	files  map[string]*spooledMedia
	oldest []string
}

func newMediaSpool(now func() time.Time) *mediaSpool {
	return &mediaSpool{now: now, files: map[string]*spooledMedia{}}
}

// reserve names a new file. The folder starts empty on each run: nothing can claim a file left by
// an earlier one, because its token died with that process.
func (m *mediaSpool) reserve() (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dir == "" {
		dir := filepath.Join(os.TempDir(), "dokja-feeds-media")
		if err := os.RemoveAll(dir); err != nil {
			return "", "", err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
		m.dir = dir
	}
	random := make([]byte, 24)
	if _, err := rand.Read(random); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(random)
	return token, filepath.Join(m.dir, token), nil
}

func (m *mediaSpool) add(token string, media *spooledMedia) {
	m.mu.Lock()
	defer m.mu.Unlock()
	media.expires = m.now().Add(mediaTTL)
	m.files[token] = media
	m.oldest = append(m.oldest, token)
	m.sweep()
}

// sweep drops what expired, then the oldest files until the spool fits. Callers hold the lock.
func (m *mediaSpool) sweep() {
	now := m.now()
	var total int64
	kept := m.oldest[:0]
	for _, token := range m.oldest {
		media, ok := m.files[token]
		if !ok {
			continue
		}
		if now.After(media.expires) {
			m.drop(token)
			continue
		}
		total += media.size
		kept = append(kept, token)
	}
	m.oldest = kept
	for total > maxSpoolBytes && len(m.oldest) > 1 {
		total -= m.files[m.oldest[0]].size
		m.drop(m.oldest[0])
		m.oldest = m.oldest[1:]
	}
}

func (m *mediaSpool) drop(token string) {
	if media, ok := m.files[token]; ok {
		_ = os.Remove(media.path)
		delete(m.files, token)
	}
}

func (m *mediaSpool) chunk(token string, offset int64) (map[string]any, error) {
	m.mu.Lock()
	m.sweep()
	media, ok := m.files[token]
	m.mu.Unlock()
	if !ok {
		return nil, reasonError(ReasonUnavailable, "media is gone")
	}
	if offset >= media.size {
		return nil, reasonError(ReasonInvalid, "offset is past the end of the media")
	}
	file, err := os.Open(media.path)
	if err != nil {
		return nil, reasonError(ReasonUnavailable, "media is gone")
	}
	defer file.Close()
	buffer := make([]byte, min(MediaChunkBytes, media.size-offset))
	if _, err := file.ReadAt(buffer, offset); err != nil {
		return nil, reasonError(ReasonFailed, "media could not be read")
	}
	done := offset+int64(len(buffer)) == media.size
	if done {
		m.mu.Lock()
		m.drop(token)
		m.mu.Unlock()
	}
	return map[string]any{"data_b64": base64.StdEncoding.EncodeToString(buffer),
		"content_type": media.contentType, "size": media.size, "done": done}, nil
}

// spoolMedia runs a reader's media_file action. The destination is always the host's own: whatever
// a caller sent under that name is replaced. A reader without the action answers without a
// content type, and that answer passes through so the caller can use the older "media" action.
func (s *Service) spoolMedia(ctx context.Context, payload map[string]any) (map[string]any, error) {
	token, path, err := s.media.reserve()
	if err != nil {
		return nil, reasonError(ReasonFailed, "media spool is unavailable")
	}
	input := map[string]any{}
	if given, ok := payload["input"].(map[string]any); ok {
		maps.Copy(input, given)
	}
	input["destination"] = path
	forwarded := maps.Clone(payload)
	forwarded["input"] = input
	result, err := s.invokePlugin(ctx, forwarded)
	if err != nil {
		_ = os.Remove(path)
		return result, err
	}
	contentType, _ := result["content_type"].(string)
	if contentType == "" {
		_ = os.Remove(path)
		return result, nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || !mediaTypes[contentType] {
		_ = os.Remove(path)
		return nil, reasonError(ReasonFailed, "reader did not write usable media")
	}
	if info.Size() > MaxMediaFileBytes {
		_ = os.Remove(path)
		return map[string]any{"refused": refusedTooBig}, nil
	}
	s.media.add(token, &spooledMedia{path: path, contentType: contentType, size: info.Size()})
	return map[string]any{"media_token": token, "content_type": contentType, "size": info.Size()}, nil
}

// readMedia answers feeds.media.read: one bounded piece of a spooled file. The last piece also
// ends the file's life here.
func (s *Service) readMedia(payload map[string]any) (map[string]any, error) {
	token, _ := payload["token"].(string)
	if !mediaToken.MatchString(token) {
		return nil, reasonError(ReasonInvalid, "token is invalid")
	}
	offset, ok := payload["offset"].(float64)
	if !ok || offset < 0 || offset != float64(int64(offset)) {
		return nil, reasonError(ReasonInvalid, "offset is invalid")
	}
	return s.media.chunk(token, int64(offset))
}
