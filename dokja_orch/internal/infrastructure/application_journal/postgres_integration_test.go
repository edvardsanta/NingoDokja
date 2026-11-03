//go:build integration

package applicationjournal

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	application "read_books/dokja_domain/dokja_memory/application"
)

func database(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DOKJA_JOURNAL_TEST_DSN")
	if dsn == "" {
		t.Fatal("integration tests require a disposable DOKJA_JOURNAL_TEST_DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open fixture database failed")
	}
	var databaseName string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&databaseName); err != nil || databaseName != "journal_test" {
		db.Close()
		t.Fatal("journal integration fixtures require the disposable journal_test database")
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	return db
}
func fixture(t *testing.T) (*sql.DB, *Store, application.Journal, application.BeginRecord) {
	t.Helper()
	db := database(t)
	s, err := Initialize(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	scope := "fixture:" + uuid.NewString()
	return db, s, journalPolicy(s, scope), beginRequest(scope)
}
func beginRequest(scope string) application.BeginRecord {
	a := application.Attempt{Stamp: application.Stamp{Contract: application.AttemptContract, Ref: "attempt:one", Scope: scope}, ApplicationRef: "application:one", WorkspaceTaskRef: "workspace:one", WorkspaceAttemptRef: "workspace-attempt:one", ProposalRef: "proposal:one", StateRevision: 1, Method: application.Binding{Ref: "method:one", Version: "v1"}, Criterion: application.Binding{Ref: "criterion:one", Version: "v1"}, InputDigest: "sha256:" + strings.Repeat("a", 64), Sources: []application.Evidence{{Ref: "source:one", Hash: "sha256:" + strings.Repeat("b", 64)}}, AuthorizationRef: "authorization:one", DispatchMode: "registered_capability", Phase: "prepared"}
	r := application.BeginRecord{Key: application.JournalKey{Scope: scope, EpisodeRef: "episode:one", AttemptRef: a.Ref, OperationKey: "operation:begin"}, Attempt: a, Constraints: []string{"private-fixture-context"}}
	return r
}
func journalPolicy(s *Store, scope string) application.Journal {
	j := application.Journal{Store: s, Clock: time.Now, Admission: application.Admission{Access: func(ctx context.Context, got, ref string) error {
		if got != scope {
			return application.ErrJournalDenied
		}
		return nil
	}, VerifyAttempt: func(a application.Attempt) bool { return a.AuthorizationRef == "authorization:one" }, VerifyObservation: func(a application.Attempt, o application.Observation) bool { return o.Observer == "producer:one" }}}
	return j
}

func dispatch(t *testing.T, j application.Journal, r application.BeginRecord) application.ChangeRecord {
	t.Helper()
	if _, err := j.Begin(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	c := application.ChangeRecord{Key: r.Key, ExpectedRevision: 1}
	c.Key.OperationKey = "operation:dispatch"
	if _, err := j.MarkDispatched(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.ExpectedRevision = 2
	return c
}
func observation(r application.BeginRecord, c application.ChangeRecord) application.ObserveRecord {
	c.Key.OperationKey = "operation:observe"
	o := application.Observation{Stamp: application.Stamp{Contract: application.ObservationContract, Ref: "observation:one", Scope: r.Key.Scope}, ApplicationRef: r.Attempt.ApplicationRef, AttemptRef: r.Attempt.Ref, InputDigest: r.Attempt.InputDigest, Criterion: r.Attempt.Criterion, Kind: "executor_receipt", Observer: "producer:one", Outcome: "succeeded", Evidence: []application.Evidence{{Ref: "receipt:one", Hash: "sha256:" + strings.Repeat("c", 64)}}}
	return application.ObserveRecord{ChangeRecord: c, Observation: o}
}

func TestPostgresReopenAndHistoricalReplay(t *testing.T) {
	db, s, j, r := fixture(t)
	ctx := context.Background()
	c := dispatch(t, j, r)
	o := observation(r, c)
	first, err := j.Observe(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = database(t)
	reopened, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	j.Store = reopened
	latest, found, err := reopened.Read(ctx, r.Key.Scope, r.Key.EpisodeRef)
	if err != nil || !found || latest.Revision != 3 || len(latest.Observations) != 1 {
		t.Fatal("reopen lost state", err)
	}
	replay, err := j.Begin(ctx, r)
	if err != nil || !replay.Replayed || replay.Episode.Revision != 1 {
		t.Fatal("replay did not retain original result", err)
	}
	receiptReplay, err := j.Observe(ctx, o)
	if err != nil || !receiptReplay.Replayed || !receiptReplay.Episode.AcceptedAt.Equal(first.Episode.AcceptedAt) {
		t.Fatal("receipt replay changed acceptance time", err)
	}
	latest.Observations[0].Outcome = "failed"
	fresh, _, err := reopened.Read(ctx, r.Key.Scope, r.Key.EpisodeRef)
	if err != nil || fresh.Observations[0].Outcome != "succeeded" {
		t.Fatal("read values alias store", err)
	}
	_ = s
}
func TestPostgresConcurrentCreationAndCAS(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		db, s, j, r := fixture(t)
		otherDB := database(t)
		otherStore, err := Open(context.Background(), otherDB)
		if err != nil {
			t.Fatal(err)
		}
		j2 := j
		j2.Store = otherStore
		var wg sync.WaitGroup
		results := make(chan application.JournalResult, 2)
		errs := make(chan error, 2)
		for _, journal := range []application.Journal{j, j2} {
			wg.Add(1)
			go func(journal application.Journal) {
				defer wg.Done()
				v, e := journal.Begin(context.Background(), r)
				results <- v
				errs <- e
			}(journal)
		}
		wg.Wait()
		close(results)
		close(errs)
		changed, replayed := 0, 0
		for v := range results {
			if v.Changed {
				changed++
			}
			if v.Replayed {
				replayed++
			}
		}
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		if changed != 1 || replayed != 1 {
			t.Fatal("concurrent absent-row creation was not idempotent")
		}
		c := application.ChangeRecord{Key: r.Key, ExpectedRevision: 1}
		c.Key.OperationKey = "operation:dispatch"
		if _, err = j.MarkDispatched(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		c.ExpectedRevision = 2
		a := observation(r, c)
		b := a
		if !duplicate {
			b.Key.OperationKey = "operation:compete"
			b.Observation.Ref = "observation:two"
			b.Observation.Outcome = "failed"
		}
		results = make(chan application.JournalResult, 2)
		errs = make(chan error, 2)
		for i, cmd := range []application.ObserveRecord{a, b} {
			wg.Add(1)
			journal := j
			if i == 1 {
				journal = j2
			}
			go func(journal application.Journal, cmd application.ObserveRecord) {
				defer wg.Done()
				v, e := journal.Observe(context.Background(), cmd)
				results <- v
				errs <- e
			}(journal, cmd)
		}
		wg.Wait()
		close(results)
		close(errs)
		changed, replayed, conflicts := 0, 0, 0
		for v := range results {
			if v.Changed {
				changed++
			}
			if v.Replayed {
				replayed++
			}
		}
		for e := range errs {
			if errors.Is(e, application.ErrJournalConflict) {
				conflicts++
			} else if e != nil {
				t.Fatal(e)
			}
		}
		if changed != 1 || (duplicate && replayed != 1) || (!duplicate && conflicts != 1) {
			t.Fatal("database CAS/replay selected the wrong writers")
		}
		e, _, err := s.Read(context.Background(), r.Key.Scope, r.Key.EpisodeRef)
		if err != nil || len(e.Observations) != 1 || e.Revision != 3 {
			t.Fatal("competing receipt leaked", err)
		}
		_ = db
	}
}
func TestPostgresFailedInsertRollsBackEveryRecord(t *testing.T) {
	db, _, j, r := fixture(t)
	ctx := context.Background()
	r.Key.OperationKey = "operation:rollback-fixture"
	_, err := db.ExecContext(ctx, `CREATE FUNCTION dokja_application_journal.reject_fixture() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation_key='operation:rollback-fixture' THEN RAISE EXCEPTION 'private fixture diagnostic'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_fixture BEFORE INSERT ON dokja_application_journal.operations FOR EACH ROW EXECUTE FUNCTION dokja_application_journal.reject_fixture();`)
	if err != nil {
		t.Fatal("create failure fixture failed")
	}
	cleanup := func() {
		db.ExecContext(ctx, `DROP TRIGGER IF EXISTS reject_fixture ON dokja_application_journal.operations; DROP FUNCTION IF EXISTS dokja_application_journal.reject_fixture();`)
	}
	defer cleanup()
	if _, err = j.Begin(ctx, r); !errors.Is(err, ErrStorage) || strings.Contains(err.Error(), "private") {
		t.Fatal("storage failure leaked details or succeeded")
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM dokja_application_journal.episodes WHERE scope_ref=$1`, r.Key.Scope).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed transaction retained head/attempt claim")
	}
	cleanup()
	if result, err := j.Begin(ctx, r); err != nil || result.Replayed {
		t.Fatal("failed operation reserved replay key", err)
	}
}
func TestPostgresCancellationBeforeCommit(t *testing.T) {
	db, s, _, r := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	e := r.Attempt
	e.AcceptedAt = time.Now()
	snapshot := application.Episode{Stamp: application.Stamp{Contract: application.EpisodeContract, Ref: r.Key.EpisodeRef, Scope: r.Key.Scope, AcceptedAt: e.AcceptedAt}, Revision: 1, Attempt: e}
	_, err := s.Transact(ctx, r.Key, func(application.JournalState) (application.JournalWrite, error) {
		cancel()
		op := application.JournalOperation{Digest: "sha256:" + strings.Repeat("d", 64), Result: snapshot}
		return application.JournalWrite{NextSnapshot: &snapshot, Operation: &op, Result: application.JournalResult{Episode: snapshot, Changed: true}}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation did not propagate", err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM dokja_application_journal.episodes WHERE scope_ref=$1`, r.Key.Scope).Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled mutation persisted")
	}
}
func TestPostgresForgetFencesReplayAndRemovesContent(t *testing.T) {
	db, s, j, r := fixture(t)
	ctx := context.Background()
	c := dispatch(t, j, r)
	if _, err := j.Observe(ctx, observation(r, c)); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(ctx, r.Key.Scope, r.Key.EpisodeRef); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Read(ctx, r.Key.Scope, r.Key.EpisodeRef); !errors.Is(err, ErrForgotten) {
		t.Fatal("forgotten content still readable", err)
	}
	if _, err := j.Begin(ctx, r); !errors.Is(err, ErrForgotten) {
		t.Fatal("replay resurrected forgotten content", err)
	}
	rekeyed := r
	rekeyed.Key.EpisodeRef = "episode:other"
	rekeyed.Key.OperationKey = "operation:other"
	if _, err := j.Begin(ctx, rekeyed); !errors.Is(err, application.ErrJournalConflict) {
		t.Fatal("forgotten attempt claim reused", err)
	}
	rekeyed.Attempt.Ref = "attempt:other"
	rekeyed.Key.AttemptRef = "attempt:other"
	rekeyed.Key.OperationKey = r.Key.OperationKey
	if _, err := j.Begin(ctx, rekeyed); !errors.Is(err, ErrForgotten) {
		t.Fatal("forgotten operation key reused", err)
	}
	var revisions, content int
	if err := db.QueryRow(`SELECT count(*) FROM dokja_application_journal.revisions WHERE scope_ref=$1`, r.Key.Scope).Scan(&revisions); err != nil || revisions != 0 {
		t.Fatal("private revisions survived forget")
	}
	if err := db.QueryRow(`SELECT count(*) FROM dokja_application_journal.operations WHERE scope_ref=$1 AND (request_digest IS NOT NULL OR result_revision IS NOT NULL OR NOT forgotten)`, r.Key.Scope).Scan(&content); err != nil || content != 0 {
		t.Fatal("replay result content survived forget")
	}
	if err := s.Forget(ctx, r.Key.Scope, r.Key.EpisodeRef); err != nil {
		t.Fatal("forget is not idempotent", err)
	}
}
func TestPostgresScopeLateReceiptAndAttemptOwnership(t *testing.T) {
	_, s, j, r := fixture(t)
	ctx := context.Background()
	c := dispatch(t, j, r)
	interrupted := c
	interrupted.Key.OperationKey = "operation:interrupt"
	if _, err := j.MarkInterrupted(ctx, interrupted); err != nil {
		t.Fatal(err)
	}
	newer := r
	newer.Key.EpisodeRef = "episode:new"
	newer.Key.OperationKey = "operation:new"
	if _, err := j.Begin(ctx, newer); !errors.Is(err, application.ErrJournalConflict) {
		t.Fatal("attempt assigned to two episodes", err)
	}
	newer.Attempt.Ref = "attempt:new"
	newer.Key.AttemptRef = newer.Attempt.Ref
	if _, err := j.Begin(ctx, newer); err != nil {
		t.Fatal(err)
	}
	old := observation(r, c)
	old.ExpectedRevision = 3
	if _, err := j.Observe(ctx, old); err != nil {
		t.Fatal(err)
	}
	current, _, err := s.Read(ctx, r.Key.Scope, newer.Key.EpisodeRef)
	if err != nil || len(current.Observations) != 0 || current.Attempt.Phase != "prepared" {
		t.Fatal("old receipt changed newer attempt", err)
	}
	if _, found, err := s.Read(ctx, "another:scope", r.Key.EpisodeRef); err != nil || found {
		t.Fatal("scope filter leaked data", err)
	}
}
func TestPostgresRejectsNewerSchemaAndCorruptSnapshot(t *testing.T) {
	db, s, j, r := fixture(t)
	ctx := context.Background()
	if _, err := j.Begin(ctx, r); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE dokja_application_journal.revisions SET snapshot=$1 WHERE scope_ref=$2`, []byte(`private fixture invalid JSON`), r.Key.Scope); err != nil {
		t.Fatal("corrupt fixture failed")
	}
	if _, _, err := s.Read(ctx, r.Key.Scope, r.Key.EpisodeRef); !errors.Is(err, ErrStorage) || strings.Contains(err.Error(), "private") {
		t.Fatal("corruption not rejected safely", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE dokja_application_journal.schema_version SET version=2 WHERE id=1`); err != nil {
		t.Fatal("version fixture failed")
	}
	defer db.ExecContext(ctx, `UPDATE dokja_application_journal.schema_version SET version=1 WHERE id=1`)
	if _, err := Open(ctx, db); !errors.Is(err, ErrSchema) {
		t.Fatal("newer schema accepted", err)
	}
	if _, err := Initialize(ctx, db); !errors.Is(err, ErrSchema) {
		t.Fatal("newer schema upgraded/downgraded", err)
	}
	if _, err := j.Begin(ctx, r); !errors.Is(err, ErrSchema) {
		t.Fatal("existing connection ignored schema version", err)
	}
}

// Fault injection at the database/sql driver boundary: commit really succeeds,
// then the driver reports a lost acknowledgment. No production fault hooks exist.
type lostAckConnector struct {
	driver.Connector
	lose *atomic.Bool
}

func (c lostAckConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &lostAckConn{Conn: conn, ConnBeginTx: conn.(driver.ConnBeginTx), QueryerContext: conn.(driver.QueryerContext), ExecerContext: conn.(driver.ExecerContext), lose: c.lose}, nil
}

type lostAckConn struct {
	driver.Conn
	driver.ConnBeginTx
	driver.QueryerContext
	driver.ExecerContext
	lose *atomic.Bool
}

func (c *lostAckConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	tx, err := c.ConnBeginTx.BeginTx(ctx, o)
	if err != nil {
		return nil, err
	}
	return lostAckTx{Tx: tx, lose: c.lose}, nil
}

type lostAckTx struct {
	driver.Tx
	lose *atomic.Bool
}

func (t lostAckTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	if t.lose.Swap(false) {
		return errors.New("fixture lost acknowledgment")
	}
	return nil
}
func TestPostgresLostCommitAcknowledgmentResolvedByReplay(t *testing.T) {
	db, _, j, r := fixture(t)
	ctx := context.Background()
	config, err := pgx.ParseConfig(os.Getenv("DOKJA_JOURNAL_TEST_DSN"))
	if err != nil {
		t.Fatal("fixture connection config invalid")
	}
	lose := &atomic.Bool{}
	faultDB := sql.OpenDB(lostAckConnector{Connector: stdlib.GetConnector(*config), lose: lose})
	defer faultDB.Close()
	store, err := Open(ctx, faultDB)
	if err != nil {
		t.Fatal(err)
	}
	j.Store = store
	lose.Store(true)
	if _, err := j.Begin(ctx, r); !errors.Is(err, ErrCommitUnknown) {
		t.Fatal("ambiguous commit falsely reported rollback/success", err)
	}
	result, err := j.Begin(ctx, r)
	if err != nil || !result.Replayed || result.Episode.Revision != 1 {
		t.Fatal("same operation did not resolve lost acknowledgment", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM dokja_application_journal.revisions WHERE scope_ref=$1`, r.Key.Scope).Scan(&count); err != nil || count != 1 {
		t.Fatal("ambiguous commit duplicated mutation")
	}
}

func TestPostgresServerRestartReplay(t *testing.T) {
	ctx := context.Background()
	scope := "fixture:server-restart"
	r := beginRequest(scope)
	if os.Getenv("DOKJA_JOURNAL_TEST_PHASE") == "read" {
		db := database(t)
		s, err := Open(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		j := journalPolicy(s, scope)
		latest, found, err := s.Read(ctx, scope, r.Key.EpisodeRef)
		if err != nil || !found || latest.Revision != 3 || len(latest.Observations) != 1 {
			t.Fatal("server/process restart lost committed journal", err)
		}
		replay, err := j.Begin(ctx, r)
		if err != nil || !replay.Replayed || replay.Episode.Revision != 1 {
			t.Fatal("restart lost original operation result", err)
		}
		c := application.ChangeRecord{Key: r.Key, ExpectedRevision: 2}
		result, err := j.Observe(ctx, observation(r, c))
		if err != nil || !result.Replayed || !result.Episode.AcceptedAt.Equal(latest.AcceptedAt) {
			t.Fatal("restart replay modified receipt", err)
		}
		return
	}
	db := database(t)
	s, err := Initialize(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	j := journalPolicy(s, scope)
	c := dispatch(t, j, r)
	if _, err := j.Observe(ctx, observation(r, c)); err != nil {
		t.Fatal(err)
	}
}
func TestPostgresInvalidWriteAndParameterizedScope(t *testing.T) {
	_, s, _, _ := fixture(t)
	ctx := context.Background()
	scope := "fixture:" + uuid.NewString() + "'; SELECT 1; --"
	r := beginRequest(scope)
	j := journalPolicy(s, scope)
	if _, err := j.Begin(ctx, r); err != nil {
		t.Fatal("bound scope data was treated as SQL", err)
	}
	r2 := beginRequest("fixture:" + uuid.NewString())
	if _, err := s.Transact(ctx, r2.Key, func(application.JournalState) (application.JournalWrite, error) {
		return application.JournalWrite{}, nil
	}); !errors.Is(err, ErrStorage) {
		t.Fatal("invalid callback result was persisted", err)
	}
	if _, found, err := s.Read(ctx, r2.Key.Scope, r2.Key.EpisodeRef); err != nil || found {
		t.Fatal("invalid callback wrote a head", err)
	}
}
