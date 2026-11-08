package price

import (
	"context"
	"sort"
	"sync"
)

// Product is what the domain knows about a watched product besides its points.
type Product struct {
	Key   string
	Title string
}

// Repository keeps histories. The durable adapter lives behind this port (see ADR 0015);
// the domain never sees how points are stored.
//
// Upsert is idempotent per history and day: writing the same observation again changes
// nothing, and a later observation of the same day replaces the earlier one.
type Repository interface {
	Upsert(ctx context.Context, observation Observation) (written int, err error)
	// History returns the points of one history between from and to, inclusive, oldest
	// first. Empty bounds are open.
	History(ctx context.Context, key Key, from, to string) ([]Point, error)
	// Keys returns every history of a product, in a stable order.
	Keys(ctx context.Context, product string) ([]Key, error)
	// Products returns the watched products, in key order.
	Products(ctx context.Context) ([]Product, error)
}

// MemoryRepository is a Repository held in process memory. It is the reference
// behavior for durable adapters and the store used by tests.
type MemoryRepository struct {
	mu     sync.Mutex
	points map[Key]map[string]Point
	titles map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{points: map[Key]map[string]Point{}, titles: map[string]string{}}
}

func (m *MemoryRepository) Upsert(_ context.Context, observation Observation) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := observation.Key()
	days := m.points[key]
	if days == nil {
		days = map[string]Point{}
		m.points[key] = days
	}
	written := 0
	for _, point := range observation.Points {
		if current, ok := days[point.On]; ok && current == point {
			continue
		}
		days[point.On] = point
		written++
	}
	m.titles[observation.Product] = observation.Title
	return written, nil
}

func (m *MemoryRepository) History(_ context.Context, key Key, from, to string) ([]Point, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	points := make([]Point, 0, len(m.points[key]))
	for on, point := range m.points[key] {
		if (from == "" || on >= from) && (to == "" || on <= to) {
			points = append(points, point)
		}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].On < points[j].On })
	return points, nil
}

func (m *MemoryRepository) Keys(_ context.Context, product string) ([]Key, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []Key
	for key := range m.points {
		if key.Product == product {
			keys = append(keys, key)
		}
	}
	sortKeys(keys)
	return keys, nil
}

func (m *MemoryRepository) Products(_ context.Context) ([]Product, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	products := make([]Product, 0, len(m.titles))
	for key, title := range m.titles {
		products = append(products, Product{Key: key, Title: title})
	}
	sort.Slice(products, func(i, j int) bool { return products[i].Key < products[j].Key })
	return products, nil
}

func sortKeys(keys []Key) {
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.Merchant != b.Merchant {
			return a.Merchant < b.Merchant
		}
		if a.Series != b.Series {
			return a.Series < b.Series
		}
		return a.Kind < b.Kind
	})
}
