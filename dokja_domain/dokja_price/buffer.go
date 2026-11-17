package price

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Mutation is a durable, ordered command. Sequence is local to one queue.
// Observations retain their original dates and decisions retain their stable IDs.
type Mutation struct {
	Sequence    int64
	Kind        string
	Observation Observation
	Watch       Watch
	Decision    DecisionMemory
}

// Apply replays a command through the existing idempotent repository contract.
func (m Mutation) Apply(ctx context.Context, target Repository) error {
	switch m.Kind {
	case "observation":
		_, err := target.Upsert(ctx, m.Observation)
		return err
	case "watch_add":
		_, err := target.AddWatch(ctx, m.Watch)
		return err
	case "watch_remove":
		_, err := target.RemoveWatch(ctx, m.Watch)
		return err
	case "decision":
		return target.SaveDecision(ctx, m.Decision)
	default:
		return errors.New("unknown price mutation")
	}
}

// DurableQueue commits its local view and pending command atomically before
// acknowledging a write. Acknowledgement removes only the pending command.
type CachedPriceView struct {
	Valid    bool
	Commands []Mutation
	Sequence int64
}

type DurableQueue interface {
	Repository
	Pending(context.Context, int) ([]Mutation, error)
	Acknowledge(context.Context, int64) error
	PendingCount(context.Context) (int, error)
	CachedView(context.Context, string) (CachedPriceView, error)
	SaveView(context.Context, string, CachedPriceView) error
	LocalChanges(context.Context, int64) ([]Mutation, error)
	Sequence(context.Context) (int64, error)
}

// BufferedRepository owns offline-first policy. Storage adapters own SQL only.
// A single background worker replays commands in order. Local writes never wait
// for remote I/O; readers merge primary data with commands awaiting delivery.
type BufferedRepository struct {
	local   DurableQueue
	mu      sync.RWMutex
	primary Repository
	syncMu  sync.Mutex
	lastOK  time.Time
}

func NewBufferedRepository(local DurableQueue) *BufferedRepository {
	return &BufferedRepository{local: local}
}
func (b *BufferedRepository) Upsert(ctx context.Context, o Observation) (int, error) {
	return b.local.Upsert(ctx, o)
}
func (b *BufferedRepository) AddWatch(ctx context.Context, w Watch) (bool, error) {
	watches, err := b.Watches(ctx)
	if err != nil {
		return false, err
	}
	exists := false
	for _, item := range watches {
		if item == w {
			exists = true
		}
	}
	_, err = b.local.AddWatch(ctx, w)
	return !exists, err
}
func (b *BufferedRepository) RemoveWatch(ctx context.Context, w Watch) (bool, error) {
	watches, err := b.Watches(ctx)
	if err != nil {
		return false, err
	}
	exists := false
	for _, item := range watches {
		if item == w {
			exists = true
		}
	}
	_, err = b.local.RemoveWatch(ctx, w)
	return exists, err
}
func (b *BufferedRepository) SaveDecision(ctx context.Context, d DecisionMemory) error {
	return b.local.SaveDecision(ctx, d)
}

// Synchronize sends a bounded batch. A failed or uncertain acknowledgement is
// retried before later commands, so an old same-day price cannot overtake a new one.
func (b *BufferedRepository) Synchronize(ctx context.Context, primary Repository, limit int) error {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	b.mu.Lock()
	b.primary = nil
	b.mu.Unlock()
	commands, err := b.local.Pending(ctx, limit)
	if err != nil {
		return err
	}
	for _, command := range commands {
		if err := command.Apply(ctx, primary); err != nil {
			return err
		}
		if err := b.local.Acknowledge(ctx, command.Sequence); err != nil {
			return err
		}
	}
	b.mu.Lock()
	b.primary = primary
	b.lastOK = time.Now().UTC()
	b.mu.Unlock()
	return nil
}

func (b *BufferedRepository) Offline() { b.mu.Lock(); b.primary = nil; b.mu.Unlock() }
func (b *BufferedRepository) PendingCount(ctx context.Context) (int, error) {
	return b.local.PendingCount(ctx)
}
func (b *BufferedRepository) LastSync() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastOK
}

// A remote read and pending snapshot cannot straddle a worker acknowledgement.
// Local writes remain independent and can append while this read is in flight.
func (b *BufferedRepository) view(ctx context.Context, key string, seed func(context.Context, Repository, *MemoryRepository) error) (Repository, error) {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.RLock()
	primary := b.primary
	b.mu.RUnlock()
	if primary == nil {
		return b.offlineView(ctx, key)
	}
	remoteCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	sequence, err := b.local.Sequence(ctx)
	if err != nil {
		return nil, err
	}
	merged := NewMemoryRepository()
	if err := seed(remoteCtx, primary, merged); err != nil {
		b.Offline()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return b.offlineView(ctx, key)
	}
	pending, err := b.local.Pending(ctx, 0)
	if err != nil {
		return nil, err
	}
	for _, command := range pending {
		if err := command.Apply(ctx, merged); err != nil {
			return nil, err
		}
	}
	if err := b.local.SaveView(ctx, key, CachedPriceView{Valid: true, Commands: merged.mutations(), Sequence: sequence}); err != nil {
		return nil, err
	}
	return merged, nil
}

func (b *BufferedRepository) History(ctx context.Context, k Key, from, to string) ([]Point, error) {
	r, err := b.view(ctx, viewKey("history", k.Product, k.Merchant, string(k.Series), string(k.Kind), from, to), func(c context.Context, p Repository, m *MemoryRepository) error {
		rows, e := p.History(c, k, from, to)
		if e != nil {
			return e
		}
		_, e = m.Upsert(c, Observation{Product: k.Product, Merchant: k.Merchant, Series: k.Series, Kind: k.Kind, Points: rows})
		return e
	})
	if err != nil {
		return nil, err
	}
	return r.History(ctx, k, from, to)
}
func (b *BufferedRepository) Keys(ctx context.Context, product string) ([]Key, error) {
	r, err := b.view(ctx, viewKey("keys", product), func(c context.Context, p Repository, m *MemoryRepository) error {
		rows, e := p.Keys(c, product)
		if e != nil {
			return e
		}
		for _, k := range rows {
			m.Upsert(c, Observation{Product: k.Product, Merchant: k.Merchant, Series: k.Series, Kind: k.Kind})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Keys(ctx, product)
}
func (b *BufferedRepository) Products(ctx context.Context) ([]Product, error) {
	r, err := b.view(ctx, "products", func(c context.Context, p Repository, m *MemoryRepository) error {
		rows, e := p.Products(c)
		if e != nil {
			return e
		}
		for _, v := range rows {
			m.Upsert(c, Observation{Product: v.Key, Title: v.Title, Category: v.Category, Brand: v.Brand, PartNumber: v.PartNumber, Specifications: v.Specifications})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Products(ctx)
}
func (b *BufferedRepository) Merchants(ctx context.Context) ([]string, error) {
	r, err := b.view(ctx, "merchants", func(c context.Context, p Repository, m *MemoryRepository) error {
		rows, e := p.Merchants(c)
		if e != nil {
			return e
		}
		for _, v := range rows {
			m.Upsert(c, Observation{Merchant: v})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Merchants(ctx)
}
func (b *BufferedRepository) OfferURLs(ctx context.Context, product string) (map[Key]string, error) {
	r, err := b.view(ctx, viewKey("offers", product), func(c context.Context, p Repository, m *MemoryRepository) error {
		rows, e := p.OfferURLs(c, product)
		if e != nil {
			return e
		}
		for k, v := range rows {
			m.Upsert(c, Observation{Product: k.Product, Merchant: k.Merchant, Series: k.Series, Kind: k.Kind, URL: v})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.OfferURLs(ctx, product)
}
func (b *BufferedRepository) Watches(ctx context.Context) ([]Watch, error) {
	r, err := b.view(ctx, "watches", func(c context.Context, p Repository, m *MemoryRepository) error {
		rows, e := p.Watches(c)
		if e != nil {
			return e
		}
		for _, v := range rows {
			m.AddWatch(c, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Watches(ctx)
}
func (b *BufferedRepository) RecentDecisions(ctx context.Context, user, product string, limit int) ([]DecisionMemory, error) {
	if limit <= 0 {
		return []DecisionMemory{}, nil
	}
	r, err := b.view(ctx, viewKey("decisions", user, product), func(c context.Context, p Repository, m *MemoryRepository) error {
		rows, e := p.RecentDecisions(c, user, product, limit)
		if e != nil {
			return e
		}
		for i := len(rows) - 1; i >= 0; i-- {
			m.SaveDecision(c, rows[i])
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.RecentDecisions(ctx, user, product, limit)
}

func (b *BufferedRepository) Online() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.primary != nil
}

// Previously read primary data is cached on disk; local tombstones and new
// observations overlay it when disconnected. Never claim a full remote mirror.
func (b *BufferedRepository) offlineView(ctx context.Context, key string) (Repository, error) {
	cached, err := b.local.CachedView(ctx, key)
	if err != nil {
		return nil, err
	}
	if !cached.Valid {
		return b.local, nil
	}
	local, err := b.local.LocalChanges(ctx, cached.Sequence)
	if err != nil {
		return nil, err
	}
	view := NewMemoryRepository()
	for _, commands := range [][]Mutation{cached.Commands, local} {
		for _, command := range commands {
			if err := command.Apply(ctx, view); err != nil {
				return nil, err
			}
		}
	}
	return view, nil
}

func (m *MemoryRepository) mutations() []Mutation {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []Mutation{}
	for key, days := range m.points {
		p := m.products[key.Product]
		o := Observation{Product: key.Product, Merchant: key.Merchant, Series: key.Series, Kind: key.Kind, Title: p.Title, Category: p.Category, Brand: p.Brand, PartNumber: p.PartNumber, Specifications: p.Specifications, URL: m.urls[key]}
		for _, point := range days {
			o.Points = append(o.Points, point)
		}
		result = append(result, Mutation{Kind: "observation", Observation: o})
	}
	for w := range m.watches {
		result = append(result, Mutation{Kind: "watch_add", Watch: w})
	}
	for _, d := range m.decisions {
		result = append(result, Mutation{Kind: "decision", Decision: d})
	}
	return result
}

func viewKey(parts ...string) string { raw, _ := json.Marshal(parts); return string(raw) }

// RunSync is the background recovery job. Preparation validates/reopens the
// configured primary and may repair its schema through the existing adapter.
func (b *BufferedRepository) RunSync(ctx context.Context, prepare func(context.Context) (Repository, error), interval time.Duration, batch int) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if batch <= 0 {
		batch = 100
	}
	delay := time.Duration(0)
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		primary, err := prepare(attempt)
		if err == nil {
			err = b.Synchronize(attempt, primary, batch)
		}
		cancel()
		delay = interval
		if err != nil {
			b.Offline()
			continue
		}
		pending, countErr := b.PendingCount(ctx)
		if countErr == nil && pending > 0 && delay > time.Second {
			delay = time.Second
		}
	}
}
