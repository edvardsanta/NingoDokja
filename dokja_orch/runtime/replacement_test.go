package runtime

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// replaceExecutable does what a build does: the running file is unlinked and a new one takes its name.
func replaceExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, path, content)
}

func TestAnUntouchedExecutableIsNotReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon")
	writeExecutable(t, path, "one")
	watch, err := WatchExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if watch.Replaced() {
			t.Fatal("the running file itself must never count as a replacement")
		}
	}
}

func TestAReplacedExecutableCountsOnlyOnceItSettles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon")
	writeExecutable(t, path, "one")
	watch, _ := WatchExecutable(path)
	replaceExecutable(t, path, "two")
	if watch.Replaced() {
		t.Fatal("the first look must only note the new file")
	}
	if !watch.Replaced() {
		t.Fatal("an unchanged new file is the replacement")
	}
}

func TestAnExecutableStillBeingWrittenIsNotStarted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon")
	writeExecutable(t, path, "one")
	watch, _ := WatchExecutable(path)
	replaceExecutable(t, path, "pa")
	watch.Replaced()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("rtial and growing"); err != nil {
		t.Fatal(err)
	}
	if watch.Replaced() {
		t.Fatal("a file that changed between two looks has not settled")
	}
}

func TestAMissingOrUnrunnableExecutableIsNotAReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon")
	writeExecutable(t, path, "one")
	watch, _ := WatchExecutable(path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	watch.Replaced()
	if watch.Replaced() {
		t.Fatal("nothing at the path is nothing to start")
	}
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	watch.Replaced()
	if watch.Replaced() {
		t.Fatal("a file that cannot run is nothing to start")
	}
}

func TestRequestsAreCountedUntilAnswered(t *testing.T) {
	active := &ActiveRequests{}
	inside := true
	handler := active.Track(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		inside = active.Idle()
	}))
	if !active.Idle() {
		t.Fatal("nothing has been asked yet")
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/request", nil))
	if inside {
		t.Fatal("a request being answered must keep the daemon busy")
	}
	if !active.Idle() {
		t.Fatal("an answered request must not keep the daemon busy")
	}
}
