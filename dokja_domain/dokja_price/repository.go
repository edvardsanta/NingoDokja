package price

import (
	"context"
	"sort"
	"sync"
)

// Product is what the domain knows about a watched product besides its points. Category,
// Brand and PartNumber come from the latest observation that carried them.
type Product struct {
	Key        string
	Title      string
	Category   string
	Brand      string
	PartNumber string
}

// Watch is a search the owner asked to follow: a category and an optional brand, in the owner's
// own words, normalized (lowercase, no accents, single spaces). Collectors run every watch.
type Watch struct {
	Category string
	Brand    string
}

// Repository keeps histories and the watched searches. The durable adapter lives behind this
// port (see ADR 0015); the domain never sees how anything is stored.
//
// Upsert is idempotent per history and day: writing the same observation again changes
// nothing, and a later observation of the same day replaces the earlier one. A product's
// description and an offer's address are replaced only by non-empty values.
type Repository interface {
	Upsert(ctx context.Context, observation Observation) (written int, err error)
	// History returns the points of one history between from and to, inclusive, oldest
	// first. Empty bounds are open.
	History(ctx context.Context, key Key, from, to string) ([]Point, error)
	// Keys returns every history of a product, in a stable order.
	Keys(ctx context.Context, product string) ([]Key, error)
	// Products returns the watched products, in key order.
	Products(ctx context.Context) ([]Product, error)
	// OfferURLs returns the known page of each history of a product that has one.
	OfferURLs(ctx context.Context, product string) (map[Key]string, error)
	// Watches returns the watched searches, in category then brand order.
	Watches(ctx context.Context) ([]Watch, error)
	// AddWatch keeps a search; added is false when it was already watched.
	AddWatch(ctx context.Context, watch Watch) (added bool, err error)
	// RemoveWatch forgets a search; removed is false when it was not watched.
	RemoveWatch(ctx context.Context, watch Watch) (removed bool, err error)
}

// MemoryRepository is a Repository held in process memory. It is the reference
// behavior for durable adapters and the store used by tests.
type MemoryRepository struct {
	mu       sync.Mutex
	points   map[Key]map[string]Point
	products map[string]Product
	urls     map[Key]string
	watches  map[Watch]bool
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		points: map[Key]map[string]Point{}, products: map[string]Product{},
		urls: map[Key]string{}, watches: map[Watch]bool{},
	}
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
	product := m.products[observation.Product]
	product.Key, product.Title = observation.Product, observation.Title
	keepNonEmpty(&product.Category, observation.Category)
	keepNonEmpty(&product.Brand, observation.Brand)
	keepNonEmpty(&product.PartNumber, observation.PartNumber)
	m.products[observation.Product] = product
	if observation.URL != "" {
		m.urls[key] = observation.URL
	}
	return written, nil
}

func keepNonEmpty(field *string, value string) {
	if value != "" {
		*field = value
	}
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
	products := make([]Product, 0, len(m.products))
	for _, product := range m.products {
		products = append(products, product)
	}
	sort.Slice(products, func(i, j int) bool { return products[i].Key < products[j].Key })
	return products, nil
}

func (m *MemoryRepository) OfferURLs(_ context.Context, product string) (map[Key]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	urls := map[Key]string{}
	for key, address := range m.urls {
		if key.Product == product {
			urls[key] = address
		}
	}
	return urls, nil
}

func (m *MemoryRepository) Watches(_ context.Context) ([]Watch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	watches := make([]Watch, 0, len(m.watches))
	for watch := range m.watches {
		watches = append(watches, watch)
	}
	sortWatches(watches)
	return watches, nil
}

func (m *MemoryRepository) AddWatch(_ context.Context, watch Watch) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.watches[watch] {
		return false, nil
	}
	m.watches[watch] = true
	return true, nil
}

func (m *MemoryRepository) RemoveWatch(_ context.Context, watch Watch) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.watches[watch] {
		return false, nil
	}
	delete(m.watches, watch)
	return true, nil
}

func sortWatches(watches []Watch) {
	sort.Slice(watches, func(i, j int) bool {
		if watches[i].Category != watches[j].Category {
			return watches[i].Category < watches[j].Category
		}
		return watches[i].Brand < watches[j].Brand
	})
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
