package runtime

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

func TestHostWorkerHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "host-worker-helper" {
		return
	}
	// Like a real worker, replace the socket a crashed predecessor left behind.
	os.Remove(os.Args[len(os.Args)-1])
	l, err := net.Listen("unix", os.Args[len(os.Args)-1])
	if err != nil {
		os.Exit(1)
	}
	defer l.Close()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		c.Close()
	}
}

func workerFixture(t *testing.T) (*hostWorkers, hostWorkerSpec) {
	t.Helper()
	root, err := os.MkdirTemp("", "dokja-worker-") // Keep Unix socket paths below the OS limit.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	h := &hostWorkers{root: root}
	t.Cleanup(func() { h.stop("feeds") })
	if err := os.MkdirAll(filepath.Join(root, ".dokja"), 0700); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec := hostWorkerSpec{ID: "reader", Module: "feeds", Directory: ".", ReadySocket: "worker.sock",
		Command: []string{binary, "-test.run=TestHostWorkerHelper", "--", "host-worker-helper", "worker.sock"}}
	writeWorkerFixture(t, root, spec)
	return h, spec
}

func writeWorkerFixture(t *testing.T, root string, spec hostWorkerSpec) {
	t.Helper()
	raw, err := json.Marshal([]hostWorkerSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".dokja", "host-workers.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestHostWorkerStartsOnlyWithItsModuleAndStopsOwnedProcess(t *testing.T) {
	h, _ := workerFixture(t)
	if err := h.start(context.Background(), "memory"); err != nil {
		t.Fatal(err)
	}
	if len(h.processes) != 0 {
		t.Fatal("unrelated module started worker")
	}
	if err := h.start(context.Background(), "feeds"); err != nil {
		t.Fatal(err)
	}
	p := h.processes["reader"]
	if p == nil {
		t.Fatal("worker not owned")
	}
	if err := h.start(context.Background(), "feeds"); err != nil {
		t.Fatal(err)
	}
	if h.processes["reader"] != p {
		t.Fatal("duplicate worker")
	}
	// Editing local configuration must not orphan a process we started.
	if err := os.Remove(filepath.Join(h.root, ".dokja", "host-workers.json")); err != nil {
		t.Fatal(err)
	}
	c := &Compose{hostWorkers: h, Run: func(context.Context, ...string) ([]byte, error) { return nil, nil }}
	m, _ := domain.Lookup("feeds")
	if err := c.Stop(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("worker outlived module stop")
	}
	if len(h.processes) != 0 {
		t.Fatal("worker still owned after stop")
	}
}

func TestHostWorkerDoesNotStopAnExistingUnownedWorker(t *testing.T) {
	h, _ := workerFixture(t)
	listener, err := net.Listen("unix", filepath.Join(h.root, "worker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := h.start(context.Background(), "feeds"); err != nil {
		t.Fatal(err)
	}
	if len(h.processes) != 0 {
		t.Fatal("adopted ownership of external process")
	}
	if err := h.stop("feeds"); err != nil {
		t.Fatal(err)
	}
	if !workerSocketReady(filepath.Join(h.root, "worker.sock")) {
		t.Fatal("external worker stopped")
	}
}

func TestHostWorkerConfigurationAndStartupFailuresAreVisible(t *testing.T) {
	h, spec := workerFixture(t)
	spec.Directory = "../outside"
	writeWorkerFixture(t, h.root, spec)
	if h.start(context.Background(), "feeds") == nil {
		t.Fatal("path outside checkout accepted")
	}
	spec.Directory = "."
	spec.Command = []string{"missing-host-worker-binary"}
	writeWorkerFixture(t, h.root, spec)
	c := &Compose{hostWorkers: h}
	m, _ := domain.Lookup("feeds")
	if err := c.Ready(context.Background(), m); err == nil || !strings.Contains(err.Error(), "host worker") {
		t.Fatal(err)
	}
}

func TestHostWorkerExitMarksModuleFailed(t *testing.T) {
	h, _ := workerFixture(t)
	if err := h.start(context.Background(), "feeds"); err != nil {
		t.Fatal(err)
	}
	stopHostProcess(h.processes["reader"])
	states := map[string]domain.Container{"dokja-feeds": {Running: true}}
	h.status(states)
	if !states["dokja-feeds"].Failed {
		t.Fatal("worker exit hidden from module status")
	}
}

func supervisedCompose(h *hostWorkers, running bool) *Compose {
	state := `{"Status":"exited","ExitCode":0}`
	if running {
		state = `{"Status":"running","Running":true}`
	}
	return &Compose{hostWorkers: h,
		Run: func(context.Context, ...string) ([]byte, error) { return []byte("aaaaaaaaaaaa"), nil },
		Inspect: func(context.Context, ...string) ([]byte, error) {
			return []byte(`[{"Id":"aaaaaaaaaaaa","Config":{"Labels":{"com.docker.compose.service":"dokja-feeds"}},"State":` + state + `}]`), nil
		}}
}

func TestSupervisionStartsMissingWorkerWhileModuleRuns(t *testing.T) {
	// A reboot or a container started outside the controller leaves no worker.
	h, _ := workerFixture(t)
	supervisedCompose(h, true).reviveHostWorkers(context.Background(), map[string]bool{})
	if h.processes["reader"] == nil || !workerSocketReady(filepath.Join(h.root, "worker.sock")) {
		t.Fatal("worker not started for a running module")
	}
}

func TestSupervisionRestartsCrashedWorker(t *testing.T) {
	h, _ := workerFixture(t)
	if err := h.start(context.Background(), "feeds"); err != nil {
		t.Fatal(err)
	}
	crashed := h.processes["reader"]
	stopHostProcess(crashed)
	supervisedCompose(h, true).reviveHostWorkers(context.Background(), map[string]bool{})
	if p := h.processes["reader"]; p == nil || p == crashed || !workerSocketReady(filepath.Join(h.root, "worker.sock")) {
		t.Fatal("crashed worker not restarted")
	}
}

func TestSupervisionLeavesStoppedModuleAndExternalWorkerAlone(t *testing.T) {
	h, _ := workerFixture(t)
	supervisedCompose(h, false).reviveHostWorkers(context.Background(), map[string]bool{})
	if len(h.processes) != 0 {
		t.Fatal("worker started for a stopped module")
	}
	listener, err := net.Listen("unix", filepath.Join(h.root, "worker.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	supervisedCompose(h, true).reviveHostWorkers(context.Background(), map[string]bool{})
	if len(h.processes) != 0 {
		t.Fatal("supervision replaced an answering external worker")
	}
}

func TestSupervisionRecordsFailureAndStopsWithContext(t *testing.T) {
	h, spec := workerFixture(t)
	spec.Command = []string{"missing-host-worker-binary"}
	writeWorkerFixture(t, h.root, spec)
	failing := map[string]bool{}
	c := supervisedCompose(h, true)
	c.reviveHostWorkers(context.Background(), failing)
	if !failing["feeds"] {
		t.Fatal("failed restart not recorded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.SuperviseHostWorkers(ctx, time.Millisecond); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervision outlived its context")
	}
}
