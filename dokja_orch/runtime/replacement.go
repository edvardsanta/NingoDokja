package runtime

import (
	"net/http"
	"os"
	"sync/atomic"
)

// ExecutableWatch tells when the file this process was started from has been replaced. A rebuild
// leaves the running daemon on the old file, unlinked, and nothing else would ever notice.
type ExecutableWatch struct {
	path    string
	running os.FileInfo
	seen    os.FileInfo
}

// WatchExecutable starts watching the file at path, which must be the one running.
func WatchExecutable(path string) (*ExecutableWatch, error) {
	running, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &ExecutableWatch{path: path, running: running}, nil
}

// Replaced reports whether another file now sits at the path. It answers true only when two looks in
// a row saw that file unchanged, so a build still writing it is never started.
func (w *ExecutableWatch) Replaced() bool {
	current, err := os.Stat(w.path)
	if err != nil || !current.Mode().IsRegular() || current.Mode().Perm()&0o100 == 0 || os.SameFile(current, w.running) {
		w.seen = nil
		return false
	}
	settled := w.seen != nil && os.SameFile(current, w.seen) && current.Size() == w.seen.Size() && current.ModTime().Equal(w.seen.ModTime())
	w.seen = current
	return settled
}

// ActiveRequests counts the requests being answered, so the daemon is replaced between them: a chat
// turn can last minutes, and the listeners it needs close once the replacement starts.
type ActiveRequests struct {
	count atomic.Int64
}

// Track counts every request the handler answers.
func (a *ActiveRequests) Track(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.count.Add(1)
		defer a.count.Add(-1)
		next.ServeHTTP(w, r)
	})
}

// Idle reports whether no request is being answered.
func (a *ActiveRequests) Idle() bool {
	return a.count.Load() == 0
}
