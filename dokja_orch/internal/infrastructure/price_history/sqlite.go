package pricehistory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
	price "read_books/dokja_domain/dokja_price"
)

// LocalQueue is a compact local replica plus an ordered outbox. Its memory cache
// is rebuilt from durable rows on reopen; a disk commit always precedes success.
type LocalQueue struct {
	price.Repository
	db *sql.DB
	mu sync.Mutex
}

func OpenLocal(path string) (*LocalQueue, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, errors.New("price buffer directory unavailable")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("price buffer unavailable")
	}
	file.Close()
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	address := url.URL{Scheme: "file", Path: absolute}
	db, err := sql.Open("sqlite", address.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, errors.New("price buffer unavailable")
	}
	db.SetMaxOpenConns(1)
	fail := func() (*LocalQueue, error) { db.Close(); return nil, errors.New("price buffer could not be opened") }
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version > 1 {
		return fail()
	}
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS price_views (
 cache_key TEXT PRIMARY KEY, payload BLOB NOT NULL, accessed INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS price_outbox (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 payload BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS price_local_cache (
 cache_key TEXT PRIMARY KEY,
 sequence INTEGER NOT NULL,
 payload BLOB NOT NULL
);
PRAGMA user_version=1;`)
	if err != nil {
		return fail()
	}
	cache := price.NewMemoryRepository()
	rows, err := db.Query(`SELECT payload FROM price_local_cache ORDER BY sequence, cache_key`)
	if err != nil {
		return fail()
	}
	for rows.Next() {
		var raw []byte
		var command price.Mutation
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &command) != nil || command.Apply(context.Background(), cache) != nil {
			rows.Close()
			return fail()
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fail()
	}
	return &LocalQueue{Repository: cache, db: db}, nil
}

func (q *LocalQueue) Close() error { return q.db.Close() }
func cacheKey(value any) string    { raw, _ := json.Marshal(value); return string(raw) }

func (q *LocalQueue) persist(ctx context.Context, command price.Mutation) error {
	raw, err := json.Marshal(command)
	if err != nil {
		return errors.New("price buffer cannot encode record")
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO price_outbox(payload) VALUES (?)`, raw)
	if err != nil {
		return safeError(ctx)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return safeError(ctx)
	}
	entries := map[string]price.Mutation{}
	switch command.Kind {
	case "observation":
		o := command.Observation
		// Preserve previously known non-empty metadata when replacing a daily cache row.
		products, _ := q.Repository.Products(ctx)
		for _, p := range products {
			if p.Key == o.Product {
				if o.Category == "" {
					o.Category = p.Category
				}
				if o.Brand == "" {
					o.Brand = p.Brand
				}
				if o.PartNumber == "" {
					o.PartNumber = p.PartNumber
				}
				if len(o.Specifications) == 0 {
					o.Specifications = p.Specifications
				}
				break
			}
		}
		if o.URL == "" {
			urls, _ := q.Repository.OfferURLs(ctx, o.Product)
			o.URL = urls[o.Key()]
		}
		points := o.Points
		for _, point := range points {
			copy := command
			o.Points = []price.Point{point}
			copy.Observation = o
			entries[cacheKey([]any{"observation", o.Key(), point.On})] = copy
		}
		if len(points) == 0 {
			command.Observation = o
			entries[cacheKey([]any{"observation", o.Key(), ""})] = command
		}
	case "watch_add", "watch_remove":
		entries[cacheKey([]any{"watch", command.Watch})] = command
	case "decision":
		entries[cacheKey([]any{"decision", command.Decision.ID})] = command
	default:
		return errors.New("unknown price command")
	}
	for key, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return errors.New("price buffer cannot encode record")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_local_cache(cache_key,sequence,payload) VALUES (?,?,?) ON CONFLICT(cache_key) DO UPDATE SET sequence=excluded.sequence,payload=excluded.payload`, key, sequence, encoded); err != nil {
			return safeError(ctx)
		}
	}
	if err := tx.Commit(); err != nil {
		return safeError(ctx)
	}
	return nil
}
func (q *LocalQueue) Upsert(ctx context.Context, o price.Observation) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.persist(ctx, price.Mutation{Kind: "observation", Observation: o}); err != nil {
		return 0, err
	}
	return q.Repository.Upsert(context.WithoutCancel(ctx), o)
}
func (q *LocalQueue) AddWatch(ctx context.Context, w price.Watch) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.persist(ctx, price.Mutation{Kind: "watch_add", Watch: w}); err != nil {
		return false, err
	}
	return q.Repository.AddWatch(context.WithoutCancel(ctx), w)
}
func (q *LocalQueue) RemoveWatch(ctx context.Context, w price.Watch) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := q.persist(ctx, price.Mutation{Kind: "watch_remove", Watch: w}); err != nil {
		return false, err
	}
	return q.Repository.RemoveWatch(context.WithoutCancel(ctx), w)
}
func (q *LocalQueue) SaveDecision(ctx context.Context, d price.DecisionMemory) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if d.ID == "" {
		return errors.New("price decision needs a stable ID")
	}
	if d.CreatedAt == "" {
		d.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	var exists int
	err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM price_local_cache WHERE cache_key=?`, cacheKey([]any{"decision", d.ID})).Scan(&exists)
	if err != nil {
		return safeError(ctx)
	}
	if exists > 0 {
		return nil
	}
	if err := q.persist(ctx, price.Mutation{Kind: "decision", Decision: d}); err != nil {
		return err
	}
	return q.Repository.SaveDecision(context.WithoutCancel(ctx), d)
}
func (q *LocalQueue) Pending(ctx context.Context, limit int) ([]price.Mutation, error) {
	if limit <= 0 {
		limit = int(^uint(0) >> 1)
	}
	rows, err := q.db.QueryContext(ctx, `SELECT sequence,payload FROM price_outbox ORDER BY sequence LIMIT ?`, limit)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer rows.Close()
	result := []price.Mutation{}
	for rows.Next() {
		var seq int64
		var raw []byte
		var item price.Mutation
		if rows.Scan(&seq, &raw) != nil || json.Unmarshal(raw, &item) != nil {
			return nil, errors.New("price buffer record is invalid")
		}
		item.Sequence = seq
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, safeError(ctx)
	}
	return result, nil
}
func (q *LocalQueue) Acknowledge(ctx context.Context, sequence int64) error {
	_, err := q.db.ExecContext(ctx, `DELETE FROM price_outbox WHERE sequence=?`, sequence)
	if err != nil {
		return safeError(ctx)
	}
	return nil
}
func (q *LocalQueue) PendingCount(ctx context.Context) (int, error) {
	var count int
	err := q.db.QueryRowContext(ctx, `SELECT count(*) FROM price_outbox`).Scan(&count)
	if err != nil {
		return 0, safeError(ctx)
	}
	return count, nil
}

func (q *LocalQueue) CachedView(ctx context.Context, key string) (price.CachedPriceView, error) {
	var payload []byte
	err := q.db.QueryRowContext(ctx, `SELECT payload FROM price_views WHERE cache_key=?`, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return price.CachedPriceView{}, nil
	}
	if err != nil {
		return price.CachedPriceView{}, safeError(ctx)
	}
	var result price.CachedPriceView
	if json.Unmarshal(payload, &result) != nil {
		return price.CachedPriceView{}, errors.New("price view cache is invalid")
	}
	return result, nil
}
func (q *LocalQueue) SaveView(ctx context.Context, key string, commands price.CachedPriceView) error {
	payload, err := json.Marshal(commands)
	if err != nil {
		return errors.New("price view cache cannot encode record")
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError(ctx)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_views(cache_key,payload,accessed) VALUES (?,?,?) ON CONFLICT(cache_key) DO UPDATE SET payload=excluded.payload,accessed=excluded.accessed`, key, payload, time.Now().UnixNano()); err != nil {
		return safeError(ctx)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM price_views WHERE cache_key IN (SELECT cache_key FROM price_views ORDER BY accessed DESC LIMIT -1 OFFSET 256)`); err != nil {
		return safeError(ctx)
	}
	if tx.Commit() != nil {
		return safeError(ctx)
	}
	return nil
}
func (q *LocalQueue) LocalChanges(ctx context.Context, after int64) ([]price.Mutation, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT payload FROM price_local_cache WHERE sequence > ? ORDER BY sequence,cache_key`, after)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer rows.Close()
	result := []price.Mutation{}
	for rows.Next() {
		var raw []byte
		var item price.Mutation
		if rows.Scan(&raw) != nil || json.Unmarshal(raw, &item) != nil {
			return nil, errors.New("price cache record is invalid")
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, safeError(ctx)
	}
	return result, nil
}

func (q *LocalQueue) Sequence(ctx context.Context) (int64, error) {
	var seq int64
	err := q.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='price_outbox'),0)`).Scan(&seq)
	if err != nil {
		return 0, safeError(ctx)
	}
	return seq, nil
}
