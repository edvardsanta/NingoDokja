package ingress

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	digest "read_books/dokja_domain/dokja_digest"

	"github.com/gofiber/fiber/v2"
)

type piecesSource struct {
	contents []byte
	piece    int
	failAt   int64
	offsets  []int64
}

func (s *piecesSource) MediaChunk(_ context.Context, _ string, offset int64) (digest.MediaChunk, error) {
	s.offsets = append(s.offsets, offset)
	if s.failAt > 0 && offset >= s.failAt {
		return digest.MediaChunk{}, errors.New("gone")
	}
	if offset >= int64(len(s.contents)) {
		return digest.MediaChunk{}, errors.New("past the end")
	}
	end := min(int(offset)+s.piece, len(s.contents))
	return digest.MediaChunk{Data: s.contents[offset:end], ContentType: "video/mp4", Size: int64(len(s.contents))}, nil
}

func mediaApp(source MediaSource) *fiber.App {
	app := fiber.New()
	app.Get("/orchestrator/media/:token", (&HTTPBridgeIngress{token: "fixture-credential", media: source}).handleMedia)
	return app
}

func mediaRequest(token, credential string) *http.Request {
	req := httptest.NewRequest("GET", "/orchestrator/media/"+token, nil)
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	return req
}

func TestMediaRouteStreamsAFileInPieces(t *testing.T) {
	contents := bytes.Repeat([]byte("clip"), 300)
	source := &piecesSource{contents: contents, piece: 1000}
	res, err := mediaApp(source).Test(mediaRequest(strings.Repeat("a", 48), "fixture-credential"), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || !bytes.Equal(body, contents) {
		t.Fatal(res.StatusCode, len(body))
	}
	if res.Header.Get("Content-Type") != "video/mp4" || res.Header.Get("Content-Length") != strconv.Itoa(len(contents)) {
		t.Fatal(res.Header)
	}
	if len(source.offsets) != 2 || source.offsets[0] != 0 || source.offsets[1] != 1000 {
		t.Fatal("the file must be pulled one piece at a time", source.offsets)
	}
}

func TestMediaRouteRefusesWithoutTheGatewayCredential(t *testing.T) {
	source := &piecesSource{contents: []byte("clip"), piece: 4}
	for _, credential := range []string{"", "wrong-credential"} {
		res, err := mediaApp(source).Test(mediaRequest(strings.Repeat("a", 48), credential), -1)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != fiber.StatusUnauthorized {
			t.Fatal(res.StatusCode)
		}
	}
	if len(source.offsets) != 0 {
		t.Fatal("an unauthorized request reached Feeds")
	}
}

func TestMediaRouteAnswersNotFoundForABadOrUnknownToken(t *testing.T) {
	source := &piecesSource{}
	for _, token := range []string{"short", strings.Repeat("A", 48), strings.Repeat("a", 48)} {
		res, err := mediaApp(source).Test(mediaRequest(token, "fixture-credential"), -1)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != fiber.StatusNotFound {
			t.Fatal(token, res.StatusCode)
		}
	}
	if len(source.offsets) != 1 {
		t.Fatal("a malformed token reached Feeds", source.offsets)
	}
	res, err := mediaApp(nil).Test(mediaRequest(strings.Repeat("a", 48), "fixture-credential"), -1)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != fiber.StatusNotFound {
		t.Fatal("no source must read as not found", res.StatusCode)
	}
}

func TestMediaStreamReportsAFailureInTheMiddle(t *testing.T) {
	source := &piecesSource{contents: []byte("clipclip"), piece: 4, failAt: 4}
	stream := &mediaStream{ctx: context.Background(), cancel: func() {}, source: source, token: "t", size: 8, offset: 4, pending: []byte("clip")}
	if body, err := io.ReadAll(stream); err == nil || string(body) != "clip" {
		t.Fatal("a short file must end in an error", string(body), err)
	}
}
