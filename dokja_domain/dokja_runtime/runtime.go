// Package runtime owns local module inventory, dependencies and lifecycle policy.
// It has no process, container, network or filesystem integration of its own.
package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Module struct {
	Name         string   `json:"name"`
	Service      string   `json:"service"`
	Dependencies []string `json:"dependencies"`
	Port         int      `json:"-"`
	Internal     bool     `json:"-"`
}

// The lite stack is the initial backend. External delivery/scheduled work is not
// implicitly started by any of these modules.
var inventory = []Module{
	{Name: "orchestrator", Service: "dokja-orchestrator", Port: 5558},
	{Name: "embeddings", Service: "dokja-ollama", Internal: true},
	{Name: "embedding_init", Service: "dokja-embedding-init", Dependencies: []string{"embeddings"}, Internal: true},
	{Name: "memory", Service: "dokja-memory", Dependencies: []string{"orchestrator", "embedding_init"}, Port: 5562},
	{Name: "knowledge", Service: "dokja-knowledge", Dependencies: []string{"orchestrator", "embedding_init"}, Port: 5561},
	{Name: "meme", Service: "dokja-meme", Dependencies: []string{"orchestrator"}, Port: 5557},
	{Name: "book", Service: "dokja-book", Dependencies: []string{"orchestrator"}, Port: 8083},
	{Name: "chat_ai", Service: "dokja-chat-ai", Dependencies: []string{"orchestrator"}, Port: 8080},
	{Name: "feeds", Service: "dokja-feeds", Dependencies: []string{"orchestrator"}, Port: 5563},
	{Name: "voice", Service: "dokja-voice", Dependencies: []string{"orchestrator"}, Port: 8081},
	// Communications only reads channels and history for the desktop; it never sends.
	{Name: "communications", Service: "dokja-communications", Dependencies: []string{"orchestrator"}},
	// Recognize externally managed consumers without exposing launch controls for
	// delivery, scheduling, authentication or other interfaces in this release.
	{Name: "discord", Service: "dokja-discord", Dependencies: []string{"orchestrator", "voice"}, Internal: true},
	{Name: "scheduler", Service: "dokja-scheduler", Dependencies: []string{"orchestrator"}, Internal: true},
	{Name: "whatsapp", Service: "dokja-whatsapp", Dependencies: []string{"orchestrator"}},
	{Name: "cli", Service: "dokja-cli", Dependencies: []string{"orchestrator"}, Internal: true},
	{Name: "tui", Service: "dokja-tui", Dependencies: []string{"orchestrator"}, Internal: true},
}

func Modules() []Module { return append([]Module(nil), inventory...) }
func Lookup(name string) (Module, bool) {
	for _, m := range inventory {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}
func Plan(name string) []Module {
	var result []Module
	seen := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if seen[name] {
			return
		}
		m, ok := Lookup(name)
		if !ok {
			return
		}
		seen[name] = true
		for _, dep := range m.Dependencies {
			visit(dep)
		}
		result = append(result, m)
	}
	visit(name)
	return result
}

type Container struct {
	Running   bool
	Completed bool
	Failed    bool
}
type Backend interface {
	Status(context.Context) (map[string]Container, error)
	Start(context.Context, Module) error
	Stop(context.Context, Module) error
	Ready(context.Context, Module) error
}

type Journal struct {
	Owned map[string]bool `json:"owned"`
	Roots map[string]bool `json:"roots"`
}
type Save func(Journal) error

type State struct {
	Name         string   `json:"name"`
	State        string   `json:"state"`
	Error        string   `json:"error,omitempty"`
	Dependencies []string `json:"dependencies"`
}
type Snapshot struct {
	Modules []State `json:"modules"`
	Busy    bool    `json:"busy"`
	Error   string  `json:"error,omitempty"`
}

type Domain struct {
	mu      sync.Mutex
	backend Backend
	states  map[string]State
	owned   map[string]bool
	roots   map[string]bool
	save    Save
	busy    bool
	timeout time.Duration
}

func New(backend Backend, journal Journal, save Save) *Domain {
	owned := map[string]bool{}
	// A local journal is still input: unknown service names never become commands.
	for _, m := range inventory {
		if journal.Owned[m.Service] {
			owned[m.Service] = true
		}
	}
	roots := map[string]bool{}
	for _, m := range inventory {
		if !m.Internal && journal.Roots[m.Name] {
			roots[m.Name] = true
		}
	}
	return &Domain{backend: backend, states: map[string]State{}, owned: owned, roots: roots, save: save, timeout: 10 * time.Minute}
}
func (d *Domain) snapshotLocked() Snapshot {
	s := Snapshot{Modules: []State{}, Busy: d.busy}
	for _, m := range inventory {
		if m.Internal {
			continue
		}
		row, ok := d.states[m.Name]
		if !ok {
			row = State{Name: m.Name, State: "stopped", Dependencies: m.Dependencies}
		}
		s.Modules = append(s.Modules, row)
	}
	return s
}

func (d *Domain) List(ctx context.Context) Snapshot {
	containers, err := d.backend.Status(ctx)
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		s := d.snapshotLocked()
		s.Error = err.Error()
		return s
	}
	for _, m := range inventory {
		old := d.states[m.Name]
		if old.State == "starting" || old.State == "stopping" {
			continue
		}
		state := "stopped"
		if containers[m.Service].Failed {
			state = "failed"
		} else if containers[m.Service].Running {
			state = "running"
		}
		if old.State == "ready" && state == "running" {
			state = "ready"
		}
		if old.State == "failed" {
			state = "failed"
		}
		d.states[m.Name] = State{Name: m.Name, State: state, Error: old.Error, Dependencies: m.Dependencies}
	}
	return d.snapshotLocked()
}

// Set acknowledges a background operation. Poll List for its actual completion.
// The request's cancellation never kills an operation already accepted by the daemon.
func (d *Domain) Set(name string, enabled bool) (Snapshot, error) {
	m, ok := Lookup(name)
	if !ok || m.Internal {
		return Snapshot{}, fmt.Errorf("unknown module")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	transition := "stopping"
	if enabled {
		transition = "starting"
	}
	if d.busy {
		if d.states[name].State == transition {
			return d.snapshotLocked(), nil
		}
		return Snapshot{}, fmt.Errorf("another module operation is in progress")
	}
	d.busy = true
	d.states[name] = State{Name: name, State: transition, Dependencies: m.Dependencies}
	go d.perform(m, enabled)
	return d.snapshotLocked(), nil
}

func (d *Domain) remember(service string, owned bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	previous := d.owned[service]
	if owned {
		d.owned[service] = true
	} else {
		delete(d.owned, service)
	}
	if d.save != nil {
		copy := map[string]bool{}
		for k, v := range d.owned {
			copy[k] = v
		}
		roots := map[string]bool{}
		for k, v := range d.roots {
			roots[k] = v
		}
		if err := d.save(Journal{Owned: copy, Roots: roots}); err != nil {
			if previous {
				d.owned[service] = true
			} else {
				delete(d.owned, service)
			}
			return fmt.Errorf("cannot save runtime ownership; check local state directory permissions")
		}
	}
	return nil
}
func (d *Domain) isOwned(service string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.owned[service]
}

func (d *Domain) perform(m Module, enabled bool) {
	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()
	err := d.change(ctx, m, enabled)
	d.mu.Lock()
	defer d.mu.Unlock()
	state := "stopped"
	if enabled {
		state = "ready"
	}
	message := ""
	if err != nil {
		state = "failed"
		message = err.Error()
	}
	d.states[m.Name] = State{Name: m.Name, State: state, Error: message, Dependencies: m.Dependencies}
	d.busy = false
}
func (d *Domain) change(ctx context.Context, m Module, enabled bool) error {
	containers, err := d.backend.Status(ctx)
	if err != nil {
		return err
	}
	plan := Plan(m.Name)
	if enabled {
		d.mu.Lock()
		d.roots[m.Name] = true
		d.mu.Unlock()
		if err := d.remember(m.Service, d.isOwned(m.Service)); err != nil {
			return err
		}
		for _, item := range plan {
			if !containers[item.Service].Running && !(item.Name == "embedding_init" && containers[item.Service].Completed) {
				// Persist intent before creating a container, so a daemon restart retains
				// dependency ownership. A failed startup never stops other modules.
				if err := d.remember(item.Service, true); err != nil {
					return err
				}
				if err := d.backend.Start(ctx, item); err != nil {
					return err
				}
			}
			if err := d.backend.Ready(ctx, item); err != nil {
				return err
			}
		}
		return nil
	}
	needed := map[string]bool{}
	inPlan := map[string]bool{}
	for _, item := range plan {
		inPlan[item.Service] = true
	}
	// Include containers started outside the daemon. A borrowed shared dependency
	// must survive stopping an unrelated module.
	for _, other := range inventory {
		if other.Name == "embeddings" || other.Name == "embedding_init" || other.Name == m.Name || !containers[other.Service].Running {
			continue
		}
		d.mu.Lock()
		explicit := d.roots[other.Name]
		owned := d.owned[other.Service]
		d.mu.Unlock()
		if inPlan[other.Service] && owned && !explicit {
			continue
		}
		for _, dep := range Plan(other.Name) {
			needed[dep.Service] = true
		}
	}
	if needed[m.Service] {
		return fmt.Errorf("module is required by another running module; stop its dependents first")
	}
	for i := len(plan) - 1; i >= 0; i-- {
		item := plan[i]
		if needed[item.Service] || (item.Name != m.Name && !d.isOwned(item.Service)) {
			continue
		}
		if err := d.backend.Stop(ctx, item); err != nil {
			return err
		}
		if item.Name == m.Name {
			d.mu.Lock()
			delete(d.roots, m.Name)
			d.mu.Unlock()
		}
		if err := d.remember(item.Service, false); err != nil {
			return err
		}
	}
	return nil
}
