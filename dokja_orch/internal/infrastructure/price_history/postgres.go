// Package pricehistory is the PostgreSQL adapter of the Price domain's Repository port
// (ADR 0015). The domain decides what an observation means; this package only keeps it.
package pricehistory

import (
	"context"
	"database/sql"
	"errors"

	pricedomain "read_books/dokja_domain/dokja_price"
)

var (
	ErrStorage = errors.New("price history storage failed")
	ErrSchema  = errors.New("price history schema is unsupported or uninitialized")
)

const schemaVersion = 1

// schemaLockKey serializes concurrent initializations of this namespace.
const schemaLockKey = int64(0x0d0c_1a00_0015_0001)

// Store keeps price histories in the dokja_price schema.
type Store struct{ db *sql.DB }

var _ pricedomain.Repository = (*Store)(nil)

// safeError never exposes native SQL diagnostics, values or credentials.
func safeError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrStorage
}

// Open checks an existing namespace. It never creates or upgrades tables. The caller owns the
// connection configuration and the database's lifetime.
func Open(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrStorage
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT version FROM dokja_price.schema_version WHERE id = 1`).Scan(&version); err != nil {
		return nil, ErrSchema
	}
	if version != schemaVersion {
		return nil, ErrSchema
	}
	return &Store{db: db}, nil
}

// Initialize installs only this namespace in the supplied database. It is idempotent and is
// run by the explicit price-history-init command, never by the orchestrator's startup.
func Initialize(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrStorage
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, schemaLockKey); err != nil {
		return nil, safeError(ctx)
	}
	if _, err := tx.ExecContext(ctx, `
CREATE SCHEMA IF NOT EXISTS dokja_price;
CREATE TABLE IF NOT EXISTS dokja_price.schema_version (
	id      SMALLINT PRIMARY KEY CHECK (id = 1),
	version INTEGER NOT NULL
);`); err != nil {
		return nil, safeError(ctx)
	}
	var version int
	err = tx.QueryRowContext(ctx, `SELECT version FROM dokja_price.schema_version WHERE id = 1 FOR UPDATE`).Scan(&version)
	switch {
	case err == nil && version != schemaVersion:
		return nil, ErrSchema
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
CREATE TABLE dokja_price.products (
	product_key TEXT PRIMARY KEY,
	title       TEXT NOT NULL,
	updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE dokja_price.points (
	product_key TEXT   NOT NULL REFERENCES dokja_price.products (product_key),
	merchant    TEXT   NOT NULL,
	series      TEXT   NOT NULL CHECK (series IN ('merchant', 'market_min')),
	price_kind  TEXT   NOT NULL CHECK (price_kind IN ('pix', 'card', 'unknown')),
	observed_on DATE   NOT NULL,
	cents       BIGINT NOT NULL CHECK (cents > 0),
	list_cents  BIGINT NOT NULL DEFAULT 0 CHECK (list_cents >= 0),
	seller      TEXT   NOT NULL DEFAULT '',
	PRIMARY KEY (product_key, merchant, series, price_kind, observed_on)
);
INSERT INTO dokja_price.schema_version (id, version) VALUES (1, 1);`); err != nil {
			return nil, safeError(ctx)
		}
	case err != nil:
		return nil, safeError(ctx)
	}
	if err := tx.Commit(); err != nil {
		return nil, safeError(ctx)
	}
	return &Store{db: db}, nil
}

// Upsert keeps one point per history and day in one transaction. A point equal to the stored
// one is not rewritten, so written counts only real changes, as the memory repository does.
func (s *Store) Upsert(ctx context.Context, observation pricedomain.Observation) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, safeError(ctx)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO dokja_price.products (product_key, title) VALUES ($1, $2)
ON CONFLICT (product_key) DO UPDATE SET title = EXCLUDED.title, updated_at = now()
WHERE dokja_price.products.title IS DISTINCT FROM EXCLUDED.title`,
		observation.Product, observation.Title); err != nil {
		return 0, safeError(ctx)
	}
	written := 0
	for _, point := range observation.Points {
		result, err := tx.ExecContext(ctx, `
INSERT INTO dokja_price.points
	(product_key, merchant, series, price_kind, observed_on, cents, list_cents, seller)
VALUES ($1, $2, $3, $4, $5::date, $6, $7, $8)
ON CONFLICT (product_key, merchant, series, price_kind, observed_on) DO UPDATE
SET cents = EXCLUDED.cents, list_cents = EXCLUDED.list_cents, seller = EXCLUDED.seller
WHERE (dokja_price.points.cents, dokja_price.points.list_cents, dokja_price.points.seller)
	IS DISTINCT FROM (EXCLUDED.cents, EXCLUDED.list_cents, EXCLUDED.seller)`,
			observation.Product, observation.Merchant, string(observation.Series), string(observation.Kind),
			point.On, point.Cents, point.ListCents, point.Seller)
		if err != nil {
			return 0, safeError(ctx)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, safeError(ctx)
		}
		written += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return 0, safeError(ctx)
	}
	return written, nil
}

// History returns one history's points between from and to, inclusive, oldest first. Empty
// bounds are open.
func (s *Store) History(ctx context.Context, key pricedomain.Key, from, to string) ([]pricedomain.Point, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT to_char(observed_on, 'YYYY-MM-DD'), cents, list_cents, seller
FROM dokja_price.points
WHERE product_key = $1 AND merchant = $2 AND series = $3 AND price_kind = $4
	AND ($5 = '' OR observed_on >= $5::date)
	AND ($6 = '' OR observed_on <= $6::date)
ORDER BY observed_on`,
		key.Product, key.Merchant, string(key.Series), string(key.Kind), from, to)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer rows.Close()
	points := []pricedomain.Point{}
	for rows.Next() {
		var point pricedomain.Point
		if err := rows.Scan(&point.On, &point.Cents, &point.ListCents, &point.Seller); err != nil {
			return nil, safeError(ctx)
		}
		points = append(points, point)
	}
	if rows.Err() != nil {
		return nil, safeError(ctx)
	}
	return points, nil
}

// Keys returns every history of a product, ordered like the memory repository.
func (s *Store) Keys(ctx context.Context, product string) ([]pricedomain.Key, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT merchant, series, price_kind
FROM dokja_price.points
WHERE product_key = $1
ORDER BY merchant, series, price_kind`, product)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer rows.Close()
	var keys []pricedomain.Key
	for rows.Next() {
		key := pricedomain.Key{Product: product}
		var series, kind string
		if err := rows.Scan(&key.Merchant, &series, &kind); err != nil {
			return nil, safeError(ctx)
		}
		key.Series, key.Kind = pricedomain.Series(series), pricedomain.Kind(kind)
		keys = append(keys, key)
	}
	if rows.Err() != nil {
		return nil, safeError(ctx)
	}
	return keys, nil
}

// Products returns the watched products in key order.
func (s *Store) Products(ctx context.Context) ([]pricedomain.Product, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT product_key, title FROM dokja_price.products ORDER BY product_key`)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer rows.Close()
	products := []pricedomain.Product{}
	for rows.Next() {
		var product pricedomain.Product
		if err := rows.Scan(&product.Key, &product.Title); err != nil {
			return nil, safeError(ctx)
		}
		products = append(products, product)
	}
	if rows.Err() != nil {
		return nil, safeError(ctx)
	}
	return products, nil
}
