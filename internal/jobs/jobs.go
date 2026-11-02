// Package jobs is ROSETTA's job address: the only way timed work starts
// (AGENTS C6). PARADOX calls POST /jobs/<name>, signed (PARADOX contract
// sign/v0.2.0, section 5); ROSETTA has no timer of its own.
//
// Delivery is at least once and runs can overlap, so every call is guarded
// three ways: the signature is checked before anything else; a run ID that
// already finished is answered as done without running again; and each run
// holds a PostgreSQL advisory lock in its transaction, so a second call for
// the same job while one runs is answered at once and does nothing.
package jobs

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Rumeasiyan/paradox/sign"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Prefix is the path the job address answers under.
const Prefix = "/jobs/"

// maxBody is the largest call body read; PARADOX's are a few hundred bytes.
const maxBody = 64 << 10

// Func does one run's work inside the run's transaction and reports how many
// things it handled (systems refreshed, entries sent).
// runID is PARADOX's run, recorded with what the work keeps (a check names
// its run). An error rolls the run back, so PARADOX retries it.
type Func func(ctx context.Context, tx pgx.Tx, runID string) (count int, err error)

// Job is one job PARADOX may call.
type Job struct {
	Name string
	Run  Func
	// Then, when set, runs once Run's transaction is kept and before the answer, holding no
	// connection, for work that waits on another service (the daily post look-ups). A run PARADOX
	// sends again is already done, so Then does not run twice; a stop part-way leaves the rest to
	// the next run. It reports how many things it handled, and cannot fail the run.
	Then func(ctx context.Context) int
	lock int64
}

// Set names ROSETTA's jobs (apps/rosetta/schedules.yaml on the platform);
// each holds its own lock, so two jobs can run at once but one job cannot
// overlap itself.
func Set(jobs ...Job) map[string]Job {
	out := make(map[string]Job, len(jobs))
	for i, j := range jobs {
		j.lock = 0x52535400 + int64(i) // "RST" + n
		out[j.Name] = j
	}
	return out
}

// ParseKeys reads PARADOX_PUBLIC_KEY: PARADOX's public key, or two separated
// by a comma while PARADOX's key is being changed.
func ParseKeys(v string) ([]ed25519.PublicKey, error) {
	keys, err := sign.ParsePublicKeys(strings.ReplaceAll(v, ",", "\n"))
	if err != nil {
		return nil, fmt.Errorf("PARADOX_PUBLIC_KEY: %w", err)
	}
	return keys, nil
}

// call is the body PARADOX sends.
type call struct {
	RunID   string `json:"run_id"`
	Job     string `json:"job"`
	Attempt int    `json:"attempt"`
}

// answer is ROSETTA's reply. PARADOX keeps the first 2 KB of a failed one,
// so it never carries anything personal.
type answer struct {
	RunID  string `json:"run_id,omitempty"`
	Status string `json:"status"` // done, already_done, already_running, or an error code
	Count  int    `json:"count"`
}

// AfterTimeout bounds the after-hooks of one call.
const AfterTimeout = 30 * time.Second

// Handler serves POST /jobs/<name>. Each after-hook runs once a proven call's
// run has finished, committed or not, and before the answer: ROSETTA has no
// timer (AGENTS C6), so work that is due whenever PARADOX calls (emails)
// runs here, outside the run's transaction and its locks.
func Handler(pool *pgxpool.Pool, keys []ed25519.PublicKey, jobs map[string]Job, logger *slog.Logger, after ...func(context.Context)) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			reply(w, http.StatusMethodNotAllowed, answer{Status: "method_not_allowed"})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			reply(w, http.StatusBadRequest, answer{Status: "unreadable_body"})
			return
		}
		// The signature first: nothing else is read from an unproven caller.
		if err := sign.VerifyFromParadox(r, body, keys...); err != nil {
			logger.Warn("job call refused", "path", r.URL.Path, "error", err)
			reply(w, http.StatusUnauthorized, answer{Status: "not_authenticated"})
			return
		}
		name := strings.TrimPrefix(r.URL.Path, Prefix)
		job, known := jobs[name]
		if !known {
			reply(w, http.StatusNotFound, answer{Status: "unknown_job"})
			return
		}
		var c call
		if err := json.Unmarshal(body, &c); err != nil || c.RunID == "" || c.Job != name {
			reply(w, http.StatusBadRequest, answer{Status: "bad_call"})
			return
		}
		status, count, err := runOnce(r.Context(), pool, job, c.RunID)
		if err == nil && status == "done" && job.Then != nil {
			// Not ended by PARADOX giving up on the call: what a look-up found stays good.
			count += job.Then(context.WithoutCancel(r.Context()))
		}
		if len(after) > 0 {
			actx, cancel := context.WithTimeout(r.Context(), AfterTimeout)
			for _, f := range after {
				f(actx)
			}
			cancel()
		}
		if err != nil {
			logger.Error("job run failed", "job", name, "run_id", c.RunID, "attempt", c.Attempt, "error", err)
			reply(w, http.StatusInternalServerError, answer{RunID: c.RunID, Status: "failed"})
			return
		}
		logger.Info("job run", "job", name, "run_id", c.RunID, "attempt", c.Attempt, "status", status, "count", count)
		reply(w, http.StatusOK, answer{RunID: c.RunID, Status: status, Count: count})
	})
}

// runOnce runs job for runID unless it already finished or another run of
// the same job holds the lock.
func runOnce(ctx context.Context, pool *pgxpool.Pool, job Job, runID string) (status string, count int, err error) {
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var locked bool
		if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", job.lock).Scan(&locked); err != nil {
			return err
		}
		if !locked {
			status = "already_running"
			return nil
		}
		var finished bool
		err := tx.QueryRow(ctx, "SELECT finished_at IS NOT NULL FROM paradox_run WHERE run_id = $1", runID).Scan(&finished)
		switch {
		case err == nil && finished:
			status = "already_done"
			return nil
		case err != nil && !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO paradox_run (run_id, job) VALUES ($1, $2)
			ON CONFLICT (run_id) DO NOTHING`, runID, job.Name); err != nil {
			return err
		}
		n, err := job.Run(ctx, tx, runID)
		if err != nil {
			return err
		}
		count = n
		status = "done"
		_, err = tx.Exec(ctx, "UPDATE paradox_run SET finished_at = now() WHERE run_id = $1", runID)
		return err
	})
	return status, count, err
}

func reply(w http.ResponseWriter, code int, a answer) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(a)
}

// Nothing is a Func that does no work, for jobs whose work comes later.
func Nothing(context.Context, pgx.Tx, string) (int, error) { return 0, nil }
