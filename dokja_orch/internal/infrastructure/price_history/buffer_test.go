package pricehistory

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	price "read_books/dokja_domain/dokja_price"
)

func fixtureObservation(cents int64) price.Observation {
	return price.Observation{Product: "pn:fixture", Title: "Fixture", Merchant: "fixture", Series: price.Series("merchant"), Kind: price.Kind("pix"), Category: "components", URL: "https://example.com/item", Points: []price.Point{{On: "2026-11-17", Cents: cents}}}
}
func openQueue(t *testing.T, path string) *LocalQueue {
	t.Helper()
	q, err := OpenLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func assertHistory(t *testing.T, r price.Repository, cents int64) {
	t.Helper()
	rows, err := r.History(context.Background(), fixtureObservation(1).Key(), "", "")
	if err != nil || len(rows) != 1 || rows[0].Cents != cents {
		t.Fatal(rows, err)
	}
}

func TestBufferSurvivesRestartAndDrainsWithoutDuplicatePoints(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "buffer.db")
	local := openQueue(t, path)
	buffered := price.NewBufferedRepository(local)
	if _, err := buffered.Upsert(ctx, fixtureObservation(100)); err != nil {
		t.Fatal(err)
	}
	if _, err := buffered.Upsert(ctx, fixtureObservation(200)); err != nil {
		t.Fatal(err)
	}
	watch := price.Watch{Category: "components"}
	buffered.AddWatch(ctx, watch)
	decision := price.DecisionMemory{ID: "decision-1", UserID: "owner", Product: price.Product{Key: "pn:fixture"}, CreatedAt: "2026-11-17T10:00:00Z"}
	if err := buffered.SaveDecision(ctx, decision); err != nil {
		t.Fatal(err)
	}
	local.Close()
	local = openQueue(t, path)
	defer local.Close()
	buffered = price.NewBufferedRepository(local)
	assertHistory(t, buffered, 200)
	pending, err := local.PendingCount(ctx)
	if err != nil || pending != 4 {
		t.Fatal(pending, err)
	}
	primary := price.NewMemoryRepository()
	for i := 0; i < 3; i++ {
		if err := buffered.Synchronize(ctx, primary, 2); err != nil {
			t.Fatal(err)
		}
	}
	assertHistory(t, primary, 200)
	pending, _ = local.PendingCount(ctx)
	if pending != 0 {
		t.Fatal(pending)
	}
	watches, _ := primary.Watches(ctx)
	if !reflect.DeepEqual(watches, []price.Watch{watch}) {
		t.Fatal(watches)
	}
	decisions, _ := primary.RecentDecisions(ctx, "owner", "", 10)
	if len(decisions) != 1 || decisions[0].CreatedAt != decision.CreatedAt {
		t.Fatal(decisions)
	}
	// Delivery removes outbox entries, not the durable offline replica.
	buffered.Offline()
	assertHistory(t, buffered, 200)
	local.Close()
	local = openQueue(t, path)
	defer local.Close()
	assertHistory(t, local, 200)
}

type uncertainAck struct {
	price.DurableQueue
	fail bool
}

func (q *uncertainAck) Acknowledge(ctx context.Context, seq int64) error {
	if q.fail {
		q.fail = false
		return errors.New("ack failed")
	}
	return q.DurableQueue.Acknowledge(ctx, seq)
}
func TestRetryAfterRemoteCommitReplaysBeforeNewerSameDayPrice(t *testing.T) {
	ctx := context.Background()
	local := openQueue(t, filepath.Join(t.TempDir(), "buffer.db"))
	defer local.Close()
	q := &uncertainAck{DurableQueue: local, fail: true}
	b := price.NewBufferedRepository(q)
	primary := price.NewMemoryRepository()
	b.Upsert(ctx, fixtureObservation(100))
	if err := b.Synchronize(ctx, primary, 100); err == nil {
		t.Fatal("expected uncertain acknowledgement")
	}
	b.Upsert(ctx, fixtureObservation(300))
	if err := b.Synchronize(ctx, primary, 100); err != nil {
		t.Fatal(err)
	}
	assertHistory(t, primary, 300)
	count, _ := local.PendingCount(ctx)
	if count != 0 {
		t.Fatal(count)
	}
}

type blockedPrimary struct {
	price.Repository
	entered chan struct{}
	release chan struct{}
}

func (p *blockedPrimary) Upsert(ctx context.Context, o price.Observation) (int, error) {
	close(p.entered)
	select {
	case <-p.release:
		return p.Repository.Upsert(ctx, o)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
func TestSlowSyncDoesNotBlockDurableLocalWrites(t *testing.T) {
	ctx := context.Background()
	local := openQueue(t, filepath.Join(t.TempDir(), "buffer.db"))
	defer local.Close()
	b := price.NewBufferedRepository(local)
	b.Upsert(ctx, fixtureObservation(100))
	primary := &blockedPrimary{Repository: price.NewMemoryRepository(), entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- b.Synchronize(ctx, primary, 1) }()
	<-primary.entered
	written := make(chan error, 1)
	go func() { _, err := b.Upsert(ctx, fixtureObservation(200)); written <- err }()
	select {
	case err := <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(primary.release)
		t.Fatal("local write waited for remote")
	}
	close(primary.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	count, _ := local.PendingCount(ctx)
	if count != 1 {
		t.Fatal("new write was incorrectly acknowledged", count)
	}
}

func TestDiskFailureNeverAcknowledgesOrMutatesMemory(t *testing.T) {
	ctx := context.Background()
	q := openQueue(t, filepath.Join(t.TempDir(), "buffer.db"))
	q.Close()
	if _, err := q.Upsert(ctx, fixtureObservation(100)); err == nil {
		t.Fatal("acknowledged failed disk write")
	}
	rows, _ := q.Repository.History(ctx, fixtureObservation(100).Key(), "", "")
	if len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestPrimaryReadsAreCachedAndPendingWatchRemovalSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "buffer.db")
	q := openQueue(t, path)
	b := price.NewBufferedRepository(q)
	p := price.NewMemoryRepository()
	watch := price.Watch{Category: "existing"}
	p.AddWatch(ctx, watch)
	p.Upsert(ctx, fixtureObservation(100))
	if err := b.Synchronize(ctx, p, 100); err != nil {
		t.Fatal(err)
	}
	assertHistory(t, b, 100)
	watches, err := b.Watches(ctx)
	if err != nil || len(watches) != 1 {
		t.Fatal(watches, err)
	}
	b.RemoveWatch(ctx, watch)
	q.Close()
	q = openQueue(t, path)
	defer q.Close()
	b = price.NewBufferedRepository(q)
	assertHistory(t, b, 100)
	watches, err = b.Watches(ctx)
	if err != nil || len(watches) != 0 {
		t.Fatal("deleted watch resurrected", watches, err)
	}
	if err := b.Synchronize(ctx, p, 100); err != nil {
		t.Fatal(err)
	}
	watches, _ = p.Watches(ctx)
	if len(watches) != 0 {
		t.Fatal(watches)
	}
}

func TestCompactedDailyCacheKeepsMetadataAndDecisionIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "buffer.db")
	q := openQueue(t, path)
	first := fixtureObservation(100)
	q.Upsert(ctx, first)
	next := fixtureObservation(200)
	next.Category = ""
	next.URL = ""
	q.Upsert(ctx, next)
	decision := price.DecisionMemory{ID: "stable", UserID: "owner"}
	q.SaveDecision(ctx, decision)
	q.SaveDecision(ctx, decision)
	q.Close()
	q = openQueue(t, path)
	defer q.Close()
	products, _ := q.Products(ctx)
	if len(products) != 1 || products[0].Category != "components" {
		t.Fatal(products)
	}
	urls, _ := q.OfferURLs(ctx, first.Product)
	if urls[first.Key()] != first.URL {
		t.Fatal(urls)
	}
	decisions, _ := q.RecentDecisions(ctx, "owner", "", 10)
	if len(decisions) != 1 {
		t.Fatal(decisions)
	}
	count, _ := q.PendingCount(ctx)
	if count != 3 {
		t.Fatal(count)
	}
}

func TestBackgroundJobRecoversWithoutRestart(t *testing.T) {
	local := openQueue(t, filepath.Join(t.TempDir(), "buffer.db"))
	defer local.Close()
	b := price.NewBufferedRepository(local)
	b.Upsert(context.Background(), fixtureObservation(123))
	primary := price.NewMemoryRepository()
	available := make(chan struct{})
	attempted := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		b.RunSync(ctx, func(context.Context) (price.Repository, error) {
			select {
			case attempted <- struct{}{}:
			default:
			}
			select {
			case <-available:
				return primary, nil
			default:
				return nil, errors.New("offline")
			}
		}, 5*time.Millisecond, 100)
	}()
	<-attempted
	count, _ := local.PendingCount(ctx)
	if count != 1 {
		t.Fatal("offline job discarded pending data", count)
	}
	close(available)
	deadline := time.After(2 * time.Second)
	for {
		count, _ = local.PendingCount(ctx)
		if count == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("job did not recover")
		case <-time.After(5 * time.Millisecond):
		}
	}
	assertHistory(t, primary, 123)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("job did not stop")
	}
}

func TestOfflineSnapshotDoesNotOverwriteNewPrimaryDataWithOldAcknowledgedCache(t *testing.T) {
	ctx := context.Background()
	q := openQueue(t, filepath.Join(t.TempDir(), "buffer.db"))
	defer q.Close()
	b := price.NewBufferedRepository(q)
	p := price.NewMemoryRepository()
	b.Upsert(ctx, fixtureObservation(100))
	b.Synchronize(ctx, p, 100)
	p.Upsert(ctx, fixtureObservation(200))
	assertHistory(t, b, 200)
	b.Offline()
	assertHistory(t, b, 200)
	b.Upsert(ctx, fixtureObservation(300))
	assertHistory(t, b, 300)
}

func TestEmptyWatchSnapshotDoesNotResurrectAcknowledgedWatch(t *testing.T) {
	ctx := context.Background()
	q := openQueue(t, filepath.Join(t.TempDir(), "buffer.db"))
	defer q.Close()
	b := price.NewBufferedRepository(q)
	p := price.NewMemoryRepository()
	w := price.Watch{Category: "fixture"}
	b.AddWatch(ctx, w)
	b.Synchronize(ctx, p, 100)
	p.RemoveWatch(ctx, w)
	rows, err := b.Watches(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	b.Offline()
	rows, err = b.Watches(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}
