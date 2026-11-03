package jobs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rumeasiyan/paradox/sign"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/jackc/pgx/v5"
)

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pubText, privText, err := sign.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := sign.ParsePublicKey(pubText)
	priv, _ := sign.ParsePrivateKey(privText)
	return pub, priv
}

// request builds a job call as PARADOX sends it, signed with priv.
func request(t *testing.T, priv ed25519.PrivateKey, job, runID string) *http.Request {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"run_id": runID, "job": job, "attempt": 1, "due_at": time.Now().UTC().Format(time.RFC3339)})
	r := httptest.NewRequest(http.MethodPost, Prefix+job, bytes.NewReader(body))
	if priv != nil {
		if err := (sign.Signer{KeyID: sign.ParadoxKeyID, Key: priv, Direction: sign.ToSystem}).Sign(r, body); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func serve(h http.Handler, r *http.Request) (int, answer) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	var a answer
	json.Unmarshal(rec.Body.Bytes(), &a)
	return rec.Code, a
}

func four(daily, weekly, removal, silent Func) map[string]Job {
	return Set(Job{Name: "daily", Run: daily}, Job{Name: "weekly", Run: weekly}, Job{Name: "removal", Run: removal}, Job{Name: "silent", Run: silent})
}

func nothing() map[string]Job { return four(Nothing, Nothing, Nothing, Nothing) }

// Refusals that never reach the database.
func TestRefusedBeforeTheDatabase(t *testing.T) {
	pub, priv := keys(t)
	_, otherPriv := keys(t)
	h := Handler(nil, []ed25519.PublicKey{pub}, nothing(), nil)
	tests := []struct {
		name string
		r    *http.Request
		code int
	}{
		{"unsigned", request(t, nil, "daily", "r1"), http.StatusUnauthorized},
		{"signed with another key", request(t, otherPriv, "daily", "r1"), http.StatusUnauthorized},
		{"unknown job", request(t, priv, "delete-everything", "r1"), http.StatusNotFound},
		{"GET", httptest.NewRequest(http.MethodGet, Prefix+"daily", nil), http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		if code, _ := serve(h, tt.r); code != tt.code {
			t.Errorf("%s: %d, want %d", tt.name, code, tt.code)
		}
	}
}

func TestEveryJobIsKnown(t *testing.T) {
	for _, name := range []string{"daily", "weekly", "removal", "silent"} {
		if _, ok := nothing()[name]; !ok {
			t.Errorf("job %s missing", name)
		}
	}
}

func TestParseKeys(t *testing.T) {
	a, _, _ := sign.GenerateKey()
	b, _, _ := sign.GenerateKey()
	if k, err := ParseKeys(a + "," + b); err != nil || len(k) != 2 {
		t.Fatalf("two keys: %v %v", k, err)
	}
	if _, err := ParseKeys(""); err == nil {
		t.Fatal("empty: want an error")
	}
}

func runID(t *testing.T) string { return fmt.Sprintf("test-%s-%d", t.Name(), time.Now().UnixNano()) }

// A run is recorded once; the same run ID again is answered as done and
// does no work (at-least-once delivery).
func TestRepeatedRunIsSkipped(t *testing.T) {
	pool := dbtest.Open(t)
	pub, priv := keys(t)
	calls := 0
	count := func(context.Context, pgx.Tx, string) (int, error) { calls++; return 3, nil }
	h := Handler(pool, []ed25519.PublicKey{pub}, four(count, Nothing, Nothing, Nothing), nil)
	id := runID(t)
	if code, a := serve(h, request(t, priv, "daily", id)); code != 200 || a.Status != "done" || a.Count != 3 {
		t.Fatalf("first: %d %+v", code, a)
	}
	if code, a := serve(h, request(t, priv, "daily", id)); code != 200 || a.Status != "already_done" {
		t.Fatalf("repeat: %d %+v", code, a)
	}
	if calls != 1 {
		t.Fatalf("work ran %d times, want 1", calls)
	}
	var kind string
	var finished bool
	pool.QueryRow(context.Background(), "SELECT job, finished_at IS NOT NULL FROM paradox_run WHERE run_id = $1", id).Scan(&kind, &finished)
	if kind != "daily" || !finished {
		t.Fatalf("recorded run: kind %q finished %v", kind, finished)
	}
}

// A failed run is rolled back and answered 500, so PARADOX retries it; the
// retry then runs.
func TestFailedRunIsRetried(t *testing.T) {
	pool := dbtest.Open(t)
	pub, priv := keys(t)
	fail := true
	work := func(context.Context, pgx.Tx, string) (int, error) {
		if fail {
			return 0, errors.New("database busy")
		}
		return 1, nil
	}
	h := Handler(pool, []ed25519.PublicKey{pub}, four(Nothing, work, Nothing, Nothing), nil)
	id := runID(t)
	if code, a := serve(h, request(t, priv, "weekly", id)); code != 500 || a.Status != "failed" {
		t.Fatalf("failing run: %d %+v", code, a)
	}
	fail = false
	if code, a := serve(h, request(t, priv, "weekly", id)); code != 200 || a.Status != "done" {
		t.Fatalf("retry: %d %+v", code, a)
	}
}

// Two overlapping runs of the same job: one works, the other is answered
// at once without working.
func TestOverlappingRunsDoNotBothWork(t *testing.T) {
	pool := dbtest.Open(t)
	pub, priv := keys(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	slow := func(context.Context, pgx.Tx, string) (int, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-release
		return 0, nil
	}
	h := Handler(pool, []ed25519.PublicKey{pub}, four(Nothing, Nothing, Nothing, slow), nil)
	first := make(chan answer, 1)
	go func() { _, a := serve(h, request(t, priv, "silent", runID(t)+"-a")); first <- a }()
	<-started
	code, a := serve(h, request(t, priv, "silent", runID(t)+"-b"))
	close(release)
	if code != 200 || a.Status != "already_running" {
		t.Fatalf("overlapping run: %d %+v", code, a)
	}
	if a := <-first; a.Status != "done" {
		t.Fatalf("first run: %+v", a)
	}
	if calls != 1 {
		t.Fatalf("work ran %d times, want 1", calls)
	}
}

// The after-hooks run on every proven call, after the run, whether it did
// the work, was already done, or failed; a refused call runs none.
func TestAfterHooksRunAfterEveryCall(t *testing.T) {
	pool := dbtest.Open(t)
	pub, priv := keys(t)
	var order []string
	work := func(context.Context, pgx.Tx, string) (int, error) { order = append(order, "run"); return 1, nil }
	fail := func(context.Context, pgx.Tx, string) (int, error) {
		order = append(order, "run")
		return 0, errors.New("test failure")
	}
	after := func(context.Context) { order = append(order, "after") }
	h := Handler(pool, []ed25519.PublicKey{pub}, four(work, fail, Nothing, Nothing), nil, after)
	id := runID(t)
	serve(h, request(t, priv, "daily", id))
	serve(h, request(t, priv, "daily", id))
	serve(h, request(t, priv, "weekly", runID(t)))
	_, other := keys(t)
	serve(h, request(t, other, "daily", runID(t)))
	if got := strings.Join(order, " "); got != "run after after run after" {
		t.Fatalf("order %q", got)
	}
}
