package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/standin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type world struct {
	t    *testing.T
	pool *pgxpool.Pool
	ts   *httptest.Server
	out  *audit.Outbox
	logs *bytes.Buffer
}

func newWorld(t *testing.T) *world {
	t.Helper()
	pool := dbtest.Open(t)
	if _, err := pool.Exec(context.Background(), `DELETE FROM audit_outbox`); err != nil {
		t.Fatal(err)
	}
	s := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: "x"})
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { s.Close(); ts.Close() })
	logs := &bytes.Buffer{}
	return &world{t: t, pool: pool, ts: ts, logs: logs, out: &audit.Outbox{Pool: pool,
		Sender: audit.Pensieve{URL: ts.URL + "/pensieve", Key: standin.PensieveKey}, Logger: slog.New(slog.NewJSONHandler(logs, nil))}}
}

func (w *world) mode(mode string, params map[string]string) {
	w.t.Helper()
	b, _ := json.Marshal(map[string]any{"mode": mode, "params": params})
	res, err := http.Post(w.ts.URL+"/control/pensieve/mode", "application/json", bytes.NewReader(b))
	if err != nil {
		w.t.Fatal(err)
	}
	res.Body.Close()
}

func event() audit.Event {
	return audit.Event{Actor: audit.Actor{Subject: "dev-admin", Name: "Test Administrator", Post: audit.PostNotKnown},
		Action: audit.ActionRoleChange, ObjectType: "person", ObjectRef: "dev-keeper", Result: audit.ResultDone,
		Details: map[string]any{"change": "granted", "role": "glossary keeper", "language": "ta"}}
}

func (w *world) record(e audit.Event, at time.Time) {
	w.t.Helper()
	err := pgx.BeginFunc(context.Background(), w.pool, func(tx pgx.Tx) error { return audit.Record(context.Background(), tx, e, at) })
	if err != nil {
		w.t.Fatal(err)
	}
}

// run is one audit-catch-up, as PARADOX's call makes it.
func (w *world) run() int {
	w.t.Helper()
	n := 0
	err := pgx.BeginFunc(context.Background(), w.pool, func(tx pgx.Tx) error {
		var err error
		n, err = w.out.Job(context.Background(), tx, "run-1")
		return err
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *world) waiting() int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_outbox`).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// received reads back the entries PENSIEVE's stand-in was sent.
func (w *world) received() string {
	w.t.Helper()
	res, err := http.Get(w.ts.URL + "/control/pensieve/received")
	if err != nil {
		w.t.Fatal(err)
	}
	defer res.Body.Close()
	var b bytes.Buffer
	b.ReadFrom(res.Body)
	return b.String()
}

func TestRecordThenDrainReachesPensieveAndEmpties(t *testing.T) {
	w := newWorld(t)
	w.record(event(), time.Now())
	sys := event()
	sys.Actor = audit.System
	w.record(sys, time.Now())
	if w.waiting() != 2 {
		t.Fatalf("%d", w.waiting())
	}
	if n := w.run(); n != 2 {
		t.Fatalf("%d sent", n)
	}
	if w.waiting() != 0 {
		t.Fatalf("%d still waiting", w.waiting())
	}
	got := w.received()
	for _, want := range []string{`not known`, `system`, `role change`, `dev-keeper`} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %s", want, got)
		}
	}
}

// An entry written in a transaction that rolls back never existed.
func TestAnEntryGoesWithItsTransaction(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(ctx, tx, event(), time.Now()); err != nil {
		t.Fatal(err)
	}
	tx.Rollback(ctx)
	if w.waiting() != 0 {
		t.Fatal("kept after a rollback")
	}
}

func TestPensieveDownKeepsEntriesAndTheyGoLater(t *testing.T) {
	w := newWorld(t)
	w.record(event(), time.Now())
	w.mode("unavailable", nil)
	if n := w.run(); n != 0 {
		t.Fatalf("%d", n)
	}
	var attempts int
	var next time.Time
	w.pool.QueryRow(context.Background(), `SELECT attempts, next_try_at FROM audit_outbox`).Scan(&attempts, &next)
	if attempts != 1 || !next.After(time.Now()) {
		t.Fatalf("%d %v", attempts, next)
	}
	// Not due yet: a run sends nothing even once PENSIEVE is back.
	w.mode("normal", nil)
	if n := w.run(); n != 0 || w.waiting() != 1 {
		t.Fatalf("%d %d", n, w.waiting())
	}
	w.pool.Exec(context.Background(), `UPDATE audit_outbox SET next_try_at = now() - interval '1 minute'`)
	if n := w.run(); n != 1 || w.waiting() != 0 {
		t.Fatalf("%d %d", n, w.waiting())
	}
}

// With no key yet there is nothing to send with: entries wait, due, with no attempt counted.
func TestNoSenderYetKeepsEntriesDue(t *testing.T) {
	w := newWorld(t)
	w.record(event(), time.Now())
	sender := w.out.Sender
	w.out.Sender = nil
	if n := w.run(); n != 0 || w.waiting() != 1 {
		t.Fatalf("%d %d", n, w.waiting())
	}
	var attempts int
	w.pool.QueryRow(context.Background(), `SELECT attempts FROM audit_outbox`).Scan(&attempts)
	if attempts != 0 {
		t.Fatalf("%d attempts", attempts)
	}
	w.out.Sender = sender
	if n := w.run(); n != 1 || w.waiting() != 0 {
		t.Fatalf("%d %d", n, w.waiting())
	}
}

func TestRetryAfterIsHonoured(t *testing.T) {
	w := newWorld(t)
	w.record(event(), time.Now())
	w.mode("too_many", map[string]string{"retry_seconds": "600"})
	w.run()
	var next time.Time
	w.pool.QueryRow(context.Background(), `SELECT next_try_at FROM audit_outbox`).Scan(&next)
	if time.Until(next) < 9*time.Minute {
		t.Fatalf("%v", next)
	}
}

func TestRefusedForGoodIsKeptMarkedAndNotSentAgain(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.record(event(), time.Now())
	// As a fault in code would have written it.
	w.pool.Exec(ctx, `UPDATE audit_outbox SET details = jsonb_set(details, '{post}', '""')`)
	if n := w.run(); n != 0 {
		t.Fatalf("%d", n)
	}
	var refusal string
	var at *time.Time
	w.pool.QueryRow(ctx, `SELECT refusal, refused_at FROM audit_outbox`).Scan(&refusal, &at)
	if refusal != "missing post" || at == nil {
		t.Fatalf("%q %v", refusal, at)
	}
	before := strings.Count(w.received(), `"path"`)
	w.run()
	if after := strings.Count(w.received(), `"path"`); after != before {
		t.Fatal("sent again")
	}
}

func TestPast30DaysIsDroppedAndReported(t *testing.T) {
	w := newWorld(t)
	w.record(event(), time.Now().Add(-31*24*time.Hour))
	w.run()
	if w.waiting() != 0 {
		t.Fatalf("%d", w.waiting())
	}
	if !strings.Contains(w.logs.String(), "dropped after 30 days") {
		t.Fatal(w.logs.String())
	}
	if strings.Contains(w.received(), "dev-keeper") {
		t.Fatal("sent an entry PENSIEVE would refuse as too old")
	}
}

func TestKickSendsWithoutTheJob(t *testing.T) {
	w := newWorld(t)
	w.record(event(), time.Now())
	w.out.Kick()
	w.out.Wait()
	if w.waiting() != 0 {
		t.Fatalf("%d", w.waiting())
	}
	// Kicked while PENSIEVE is silent, the entry waits and no request is held.
	w.mode("silent", nil)
	w.record(event(), time.Now())
	w.out.Sender = audit.Pensieve{URL: w.ts.URL + "/pensieve", Key: standin.PensieveKey, Client: &http.Client{Timeout: 100 * time.Millisecond}}
	began := time.Now()
	w.out.Kick()
	if time.Since(began) > 50*time.Millisecond {
		t.Fatal("Kick held its caller")
	}
	w.out.Wait()
	if w.waiting() != 1 {
		t.Fatalf("%d", w.waiting())
	}
}

// A batch at a time, across more than one batch.
func TestDrainCrossesBatches(t *testing.T) {
	w := newWorld(t)
	for range audit.BatchSize + 5 {
		w.record(event(), time.Now())
	}
	if n := w.run(); n != audit.BatchSize+5 {
		t.Fatalf("%d", n)
	}
}

// Stored details are what was written: Record refuses what PENSIEVE would.
func TestRecordRefusesWhatMayNotGetIn(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	for name, details := range map[string]map[string]any{
		"a key":  {"api_key": "x"},
		"a card": {"note": "123456789V"},
	} {
		e := event()
		e.Details = details
		err := pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error { return audit.Record(ctx, tx, e, time.Now()) })
		if err == nil {
			t.Errorf("%s got in", name)
		}
	}
	if w.waiting() != 0 {
		t.Fatalf("%d", w.waiting())
	}
}
