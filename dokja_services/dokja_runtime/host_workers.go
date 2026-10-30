package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

// Host workers are explicitly configured by the checkout owner. The renderer can
// only toggle a known module; it can never supply an executable or its arguments.
type hostWorkerSpec struct {
	ID          string   `json:"id"`
	Module      string   `json:"module"`
	Directory   string   `json:"directory"`
	Command     []string `json:"command"`
	ReadySocket string   `json:"ready_socket"`
}

type hostProcess struct {
	cmd    *exec.Cmd
	done   chan struct{}
	module string
}

type hostWorkers struct {
	root      string
	mu        sync.Mutex
	processes map[string]*hostProcess
}

func insideRoot(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.Clean(path) != ".." && !strings.HasPrefix(filepath.Clean(path), ".."+string(filepath.Separator))
}

func (h *hostWorkers) specs(module string) ([]hostWorkerSpec, error) {
	raw, err := os.ReadFile(filepath.Join(h.root, ".dokja", "host-workers.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil || len(raw) > 65536 {
		return nil, fmt.Errorf("cannot read local host worker configuration")
	}
	var specs []hostWorkerSpec
	if json.Unmarshal(raw, &specs) != nil || len(specs) > 8 {
		return nil, fmt.Errorf("invalid local host worker configuration")
	}
	seen := map[string]bool{}
	var selected []hostWorkerSpec
	for _, spec := range specs {
		m, ok := domain.Lookup(spec.Module)
		if !ok || m.Internal || spec.ID == "" || len(spec.ID) > 64 || seen[spec.ID] || !insideRoot(spec.Directory) || !insideRoot(spec.ReadySocket) || len(spec.Command) == 0 || len(spec.Command) > 16 {
			return nil, fmt.Errorf("invalid local host worker configuration")
		}
		for _, arg := range spec.Command {
			if len(arg) > 2048 || strings.ContainsRune(arg, 0) {
				return nil, fmt.Errorf("invalid host worker command")
			}
		}
		seen[spec.ID] = true
		if spec.Module == module {
			selected = append(selected, spec)
		}
	}
	return selected, nil
}

func workerSocketReady(path string) bool {
	// Resolve a shared plugin symlink before dialing: worktree paths can exceed
	// the Unix socket path limit even when the actual socket path does not.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	conn, err := net.DialTimeout("unix", resolved, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (h *hostWorkers) start(ctx context.Context, module string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	specs, err := h.specs(module)
	if err != nil {
		return err
	}
	if h.processes == nil {
		h.processes = map[string]*hostProcess{}
	}
	for _, spec := range specs {
		socket := filepath.Join(h.root, spec.ReadySocket)
		if workerSocketReady(socket) {
			continue
		} // Never take ownership of an existing worker.
		if previous := h.processes[spec.ID]; previous != nil {
			select {
			case <-previous.done:
				delete(h.processes, spec.ID)
			default:
				return fmt.Errorf("host worker %s is not ready", spec.ID)
			}
		}
		cmd := exec.Command(spec.Command[0], spec.Command[1:]...)
		cmd.Dir = filepath.Join(h.root, spec.Directory)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("cannot start host worker %s; check its local command", spec.ID)
		}
		p := &hostProcess{cmd: cmd, done: make(chan struct{}), module: module}
		h.processes[spec.ID] = p
		go func() { _ = cmd.Wait(); close(p.done) }()
		wait, cancel := context.WithTimeout(ctx, 5*time.Second)
		for !workerSocketReady(socket) {
			select {
			case <-p.done:
				cancel()
				return fmt.Errorf("host worker %s exited before becoming ready", spec.ID)
			case <-wait.Done():
				cancel()
				stopHostProcess(p)
				return fmt.Errorf("host worker %s did not become ready", spec.ID)
			case <-time.After(25 * time.Millisecond):
			}
		}
		cancel()
	}
	return nil
}

func stopHostProcess(p *hostProcess) {
	select {
	case <-p.done:
		return
	default:
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGINT)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}

func (h *hostWorkers) stop(module string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, p := range h.processes {
		if p.module == module {
			stopHostProcess(p)
			delete(h.processes, id)
		}
	}
	return nil
}

func (h *hostWorkers) status(containers map[string]domain.Container) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.processes {
		select {
		case <-p.done:
			m, _ := domain.Lookup(p.module)
			state := containers[m.Service]
			state.Failed = true
			containers[m.Service] = state
		default:
		}
	}
}
