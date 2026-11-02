package ingress

import (
	"context"
	"io"
	"regexp"
	"time"

	digest "read_books/dokja_domain/dokja_digest"

	"github.com/gofiber/fiber/v2"
)

// mediaStreamTimeout covers one whole file, as slowly as Feeds can hand it over.
const mediaStreamTimeout = 5 * time.Minute

var mediaTokenPattern = regexp.MustCompile(`^[a-f0-9]{48}$`)

// MediaSource reads one piece of a post file.
type MediaSource interface {
	MediaChunk(ctx context.Context, token string, offset int64) (digest.MediaChunk, error)
}

// handleMedia streams a file a delivery named. It pulls one piece at a time, so a file never sits
// whole in this process. A failure past the headers breaks the connection, and the caller, who was
// told the size, sees a short file.
func (i *HTTPBridgeIngress) handleMedia(c *fiber.Ctx) error {
	if status := i.authorize(c); status != fiber.StatusOK {
		return c.SendStatus(status)
	}
	token := c.Params("token")
	if i.media == nil || !mediaTokenPattern.MatchString(token) {
		return c.SendStatus(fiber.StatusNotFound)
	}
	// The body is written after this handler returns, so the stream cannot use the request's context.
	ctx, cancel := context.WithTimeout(context.Background(), mediaStreamTimeout)
	first, err := i.media.MediaChunk(ctx, token, 0)
	if err != nil {
		cancel()
		return c.SendStatus(fiber.StatusNotFound)
	}
	c.Set(fiber.HeaderContentType, first.ContentType)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.SendStream(&mediaStream{ctx: ctx, cancel: cancel, source: i.media, token: token,
		size: first.Size, offset: int64(len(first.Data)), pending: first.Data}, int(first.Size))
}

type mediaStream struct {
	ctx     context.Context
	cancel  context.CancelFunc
	source  MediaSource
	token   string
	size    int64
	offset  int64
	pending []byte
}

func (s *mediaStream) Read(buffer []byte) (int, error) {
	if len(s.pending) == 0 {
		if s.offset >= s.size {
			return 0, io.EOF
		}
		piece, err := s.source.MediaChunk(s.ctx, s.token, s.offset)
		if err != nil {
			return 0, err
		}
		if piece.Size != s.size {
			return 0, io.ErrUnexpectedEOF
		}
		s.pending, s.offset = piece.Data, s.offset+int64(len(piece.Data))
	}
	copied := copy(buffer, s.pending)
	s.pending = s.pending[copied:]
	return copied, nil
}

func (s *mediaStream) Close() error {
	s.cancel()
	return nil
}
