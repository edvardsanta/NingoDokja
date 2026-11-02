//go:build unix

package feeds

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mediaRunner stands in for a reader: it answers what the test says after seeing the destination.
type mediaRunner struct {
	destination string
	reply       func(destination string) ([]byte, error)
}

func (r *mediaRunner) Run(_ context.Context, _ Plugin, _ []string, input []byte) ([]byte, error) {
	var request struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(input, &request); err != nil {
		return nil, err
	}
	r.destination, _ = request.Payload["destination"].(string)
	return r.reply(r.destination)
}

func mediaService(t *testing.T, runner Runner, now func() time.Time) *Service {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	root := t.TempDir()
	newPlugin(t, root, "post-reader", v1(map[string]any{"id": "post-reader", "enabled": true, "formats": nil, "provides": []string{CapabilitySocialResolve}}))
	return New(Config{PluginsDir: root, Limits: testLimits()}, Options{Runner: runner, Now: now})
}

func writes(contents []byte, contentType string) func(string) ([]byte, error) {
	return func(destination string) ([]byte, error) {
		if err := os.WriteFile(destination, contents, 0o600); err != nil {
			return nil, err
		}
		return []byte(`{"result":{"content_type":"` + contentType + `"}}`), nil
	}
}

func spool(service *Service) (map[string]any, error) {
	return service.Dispatch(context.Background(), "feeds.invoke", map[string]any{"capability": CapabilitySocialResolve, "action": ActionMediaFile,
		"plugin": "post-reader", "input": map[string]any{"url": "https://reader.example/media/1", "destination": "/etc/passwd"}})
}

func TestPostMediaIsHandedOverInBoundedPiecesAndThenForgotten(t *testing.T) {
	contents := bytes.Repeat([]byte("clip"), MediaChunkBytes/2) // two pieces
	runner := &mediaRunner{reply: writes(contents, "video/mp4")}
	service := mediaService(t, runner, nil)
	reply, err := spool(service)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(runner.destination, "passwd") || !strings.HasPrefix(runner.destination, os.TempDir()) {
		t.Fatal("the caller chose the destination:", runner.destination)
	}
	for key, value := range reply {
		if text, ok := value.(string); ok && strings.Contains(text, string(filepath.Separator)+"tmp") {
			t.Fatal("a path left the service in", key)
		}
	}
	if reply["content_type"] != "video/mp4" || reply["size"] != int64(len(contents)) {
		t.Fatal(reply)
	}
	var received []byte
	for !bytes.Equal(received, contents) {
		piece, err := service.Dispatch(context.Background(), "feeds.media.read", map[string]any{"token": reply["media_token"], "offset": float64(len(received))})
		if err != nil {
			t.Fatal(err)
		}
		data, err := base64.StdEncoding.DecodeString(piece["data_b64"].(string))
		if err != nil || len(data) == 0 || len(data) > MediaChunkBytes {
			t.Fatal(len(data), err)
		}
		received = append(received, data...)
		if piece["done"] != (len(received) == len(contents)) {
			t.Fatal("done must mark the last piece", piece["done"], len(received))
		}
	}
	if _, err := os.Stat(runner.destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the file outlived its last piece", err)
	}
	if _, err := service.Dispatch(context.Background(), "feeds.media.read", map[string]any{"token": reply["media_token"], "offset": float64(0)}); err == nil {
		t.Fatal("a finished token must not read again")
	}
}

func TestAReaderWithoutMediaFilePassesThroughForTheOlderAction(t *testing.T) {
	runner := &mediaRunner{reply: func(string) ([]byte, error) { return []byte(`{"result":{"handled":false}}`), nil }}
	reply, err := spool(mediaService(t, runner, nil))
	if err != nil || reply["handled"] != false || reply["media_token"] != nil {
		t.Fatal(reply, err)
	}
}

func TestAReadersMediaFileRefusalReachesTheCaller(t *testing.T) {
	runner := &mediaRunner{reply: func(string) ([]byte, error) { return []byte(`{"result":{"refused":"media_too_large"}}`), nil }}
	reply, err := spool(mediaService(t, runner, nil))
	if err != nil || reply["refused"] != "media_too_large" {
		t.Fatal(reply, err)
	}
}

func TestSpoolRefusesWhatItCannotDeliver(t *testing.T) {
	tooBig := func(destination string) ([]byte, error) {
		file, err := os.Create(destination)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		if err := file.Truncate(MaxMediaFileBytes + 1); err != nil {
			return nil, err
		}
		return []byte(`{"result":{"content_type":"video/mp4"}}`), nil
	}
	link := func(destination string) ([]byte, error) {
		if err := os.Symlink("/etc/hostname", destination); err != nil {
			return nil, err
		}
		return []byte(`{"result":{"content_type":"video/mp4"}}`), nil
	}
	refused := &mediaRunner{reply: tooBig}
	if reply, err := spool(mediaService(t, refused, nil)); err != nil || reply["refused"] != "media_too_large" {
		t.Fatal(reply, err)
	}
	if _, err := os.Stat(refused.destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a refused file stayed behind", err)
	}
	for name, reply := range map[string]func(string) ([]byte, error){
		"empty": writes(nil, "video/mp4"), "wrong type": writes([]byte("x"), "text/html"), "symlink": link,
	} {
		runner := &mediaRunner{reply: reply}
		if result, err := spool(mediaService(t, runner, nil)); err == nil {
			t.Fatal(name, result)
		}
		if _, err := os.Lstat(runner.destination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal(name, "stayed behind", err)
		}
	}
}

func TestSpooledMediaExpires(t *testing.T) {
	now := time.Now()
	runner := &mediaRunner{reply: writes([]byte("clip"), "video/mp4")}
	service := mediaService(t, runner, func() time.Time { return now })
	reply, err := spool(service)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(mediaTTL + time.Second)
	if _, err := service.Dispatch(context.Background(), "feeds.media.read", map[string]any{"token": reply["media_token"], "offset": float64(0)}); err == nil {
		t.Fatal("an expired token must not read")
	}
	if _, err := os.Stat(runner.destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an expired file stayed behind", err)
	}
}

func TestSpoolStaysWithinItsBudget(t *testing.T) {
	sparse := func(destination string) ([]byte, error) {
		file, err := os.Create(destination)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		if err := file.Truncate(MaxMediaFileBytes); err != nil {
			return nil, err
		}
		return []byte(`{"result":{"content_type":"video/mp4"}}`), nil
	}
	runner := &mediaRunner{reply: sparse}
	service := mediaService(t, runner, nil)
	var first string
	for index := range maxSpoolBytes/MaxMediaFileBytes + 1 {
		if _, err := spool(service); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			first = runner.destination
		}
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the oldest file should have made room", err)
	}
	if _, err := os.Stat(runner.destination); err != nil {
		t.Fatal("the newest file must stay", err)
	}
}

func TestMediaReadRefusesABadRequest(t *testing.T) {
	service := mediaService(t, &mediaRunner{}, nil)
	token := strings.Repeat("a", 48)
	for _, payload := range []map[string]any{
		{"token": "../../etc/passwd", "offset": float64(0)}, {"offset": float64(0)}, {"token": token}, {"token": token, "offset": float64(-1)}, {"token": token, "offset": 1.5},
		{"token": token, "offset": float64(0)},
	} {
		if result, err := service.Dispatch(context.Background(), "feeds.media.read", payload); err == nil {
			t.Fatal(payload, result)
		}
	}
}
