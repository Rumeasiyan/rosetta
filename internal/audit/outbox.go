package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The outbox's limits. KeepFor is the PRD's (Annex A) and PENSIEVE's late limit.
// A run holds a transaction, and the platform ends one left idle for 60 seconds,
// so it stops in good time; what is left goes on the next call.
const (
	KeepFor    = 30 * 24 * time.Hour
	BatchSize  = 100
	RunRows    = 500
	RunFor     = 30 * time.Second
	KickFor    = 15 * time.Second
	MaxBackoff = time.Hour
)

// Result is PENSIEVE's answer for one entry.
type Result struct {
	ID     string `json:"id"`
	Status string `json:"status"` // kept or refused
	Reason string `json:"reason"`
	Item   string `json:"item"`
}

// RetryAfter is PENSIEVE saying when to try again. Nothing was kept.
type RetryAfter struct{ Wait time.Duration }

func (r RetryAfter) Error() string { return fmt.Sprintf("try again after %s", r.Wait) }

// TooLarge is PENSIEVE saying a request held too much. Nothing was kept. Limit is
// its batch limit, or zero when it gave none.
type TooLarge struct{ Limit int }

func (t TooLarge) Error() string { return "the request held too much" }

// Sender delivers entries. An error means nothing can be said about any of them.
type Sender interface {
	Send(ctx context.Context, entries []json.RawMessage) ([]Result, error)
}

// Pensieve is PENSIEVE's POST /api/entries (docs/INTAKE.md in its repository). Its
// address and key are settings; locally they name the stand-in.
type Pensieve struct {
	URL    string
	Key    string // never logged
	Client *http.Client
}

// Send makes one request. Its errors name a status, never a key or an entry.
func (p Pensieve) Send(ctx context.Context, entries []json.RawMessage) ([]Result, error) {
	body, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL+"/api/entries", bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("PENSIEVE's address is not usable")
	}
	req.Header.Set("Authorization", "Bearer "+p.Key)
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("PENSIEVE could not be reached")
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	switch res.StatusCode {
	case http.StatusOK, http.StatusMultiStatus, http.StatusUnprocessableEntity:
		var answer struct {
			Results []Result `json:"results"`
		}
		if err := json.Unmarshal(raw, &answer); err != nil {
			return nil, errors.New("PENSIEVE's answer is not JSON")
		}
		return answer.Results, nil
	case http.StatusTooManyRequests:
		var answer struct {
			RetryAfter int `json:"retry_after"`
		}
		json.Unmarshal(raw, &answer)
		seconds, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After")))
		if err != nil {
			seconds = answer.RetryAfter
		}
		if seconds <= 0 {
			seconds = 60
		}
		return nil, RetryAfter{Wait: time.Duration(seconds) * time.Second}
	case http.StatusRequestEntityTooLarge:
		var answer struct {
			Limit int `json:"limit"`
		}
		json.Unmarshal(raw, &answer)
		return nil, TooLarge{Limit: answer.Limit}
	}
	return nil, fmt.Errorf("PENSIEVE answered status %d", res.StatusCode)
}

// backoff is the wait after a failure: it grows with the tries, up to an hour.
func backoff(tries int) time.Duration {
	d := time.Minute
	for i := 1; i < tries && d < MaxBackoff; i++ {
		d *= 2
	}
	return min(d, MaxBackoff)
}

// row is one entry due to go.
type row struct {
	id    int64
	entry string // the entry's own id
	body  json.RawMessage
	tries int
}

// outcome is what became of one row in one attempt.
type outcome struct {
	kept    bool
	refusal string        // refused for good: PENSIEVE's reason and item
	wait    time.Duration // otherwise, try again after this
}

// deliver sends rows and says what became of each. stopped says the rest of this
// run should wait too: PENSIEVE cannot take anything now.
func deliver(ctx context.Context, s Sender, rows []row, logger *slog.Logger) (map[int64]outcome, bool) {
	out := make(map[int64]outcome, len(rows))
	bodies := make([]json.RawMessage, len(rows))
	for i, r := range rows {
		bodies[i] = r.body
	}
	results, err := s.Send(ctx, bodies)
	var tooLarge TooLarge
	var retry RetryAfter
	switch {
	case errors.As(err, &tooLarge) && len(rows) > 1:
		size := len(rows) / 2
		if tooLarge.Limit > 0 && tooLarge.Limit < len(rows) {
			size = tooLarge.Limit
		}
		stopped := false
		for from := 0; from < len(rows) && !stopped; from += size {
			part, stop := deliver(ctx, s, rows[from:min(from+size, len(rows))], logger)
			for id, o := range part {
				out[id] = o
			}
			stopped = stop
		}
		return out, stopped
	case errors.As(err, &retry):
		for _, r := range rows {
			out[r.id] = outcome{wait: retry.Wait}
		}
		return out, true
	case err != nil:
		logger.Warn("audit entries wait", "count", len(rows), "error", err)
		for _, r := range rows {
			out[r.id] = outcome{wait: backoff(r.tries + 1)}
		}
		return out, true
	}
	byID := make(map[string]Result, len(results))
	for _, x := range results {
		byID[x.ID] = x
	}
	for _, r := range rows {
		x, answered := byID[r.entry]
		switch {
		case answered && x.Status == "kept":
			out[r.id] = outcome{kept: true}
		case answered && x.Status == "refused" && x.Reason != "not_kept":
			// A fault in the code that wrote it: the same again gets the same answer.
			refusal := x.Reason
			if x.Item != "" {
				refusal += " " + x.Item
			}
			logger.Error("audit entry refused by PENSIEVE", "entry_id", r.entry, "refusal", refusal)
			out[r.id] = outcome{refusal: refusal}
		default:
			out[r.id] = outcome{wait: backoff(r.tries + 1)}
		}
	}
	return out, false
}

// Waiting counts what has not reached PENSIEVE and what it refused for good, for the status
// screen: the only place, beyond the log, a person sees these.
func Waiting(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) (waiting, refused int, err error) {
	err = q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE refused_at IS NULL), count(*) FILTER (WHERE refused_at IS NOT NULL) FROM audit_outbox`).Scan(&waiting, &refused)
	return waiting, refused, err
}

// DropExpired removes each entry past KeepFor, sent or not, which PENSIEVE would refuse as too old
// (Annex A: at most 30 days), and says so in the log. A sent entry's row is gone already. It reports
// how many went.
func DropExpired(ctx context.Context, tx pgx.Tx, logger *slog.Logger, now time.Time) (int, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM audit_outbox WHERE occurred_at < $1`, now.Add(-KeepFor))
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	if n > 0 {
		logger.Error("audit entries dropped after 30 days without reaching PENSIEVE", "count", n)
	}
	return n, nil
}

// Drain sends what is due, oldest first, and reports how many PENSIEVE now holds.
// An entry that cannot go waits, and tells the next run when; an entry past KeepFor
// is removed and reported, since PENSIEVE would refuse it as too old.
func Drain(ctx context.Context, tx pgx.Tx, s Sender, logger *slog.Logger, now time.Time) (int, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if _, err := DropExpired(ctx, tx, logger, now); err != nil {
		return 0, err
	}
	sent, handled := 0, 0
	began := time.Now()
	for handled < RunRows && time.Since(began) < RunFor {
		rows, err := dueRows(ctx, tx, now)
		if err != nil {
			return sent, err
		}
		if len(rows) == 0 {
			break
		}
		outcomes, stopped := deliver(ctx, s, rows, logger)
		kept, err := apply(ctx, tx, rows, outcomes, now)
		if err != nil {
			return sent, err
		}
		sent += kept
		handled += len(rows)
		if stopped {
			break
		}
	}
	return sent, nil
}

func dueRows(ctx context.Context, tx pgx.Tx, now time.Time) ([]row, error) {
	rows, err := tx.Query(ctx, `SELECT id, entry_id, details::text, attempts FROM audit_outbox
		WHERE refused_at IS NULL AND next_try_at <= $1 ORDER BY occurred_at, id LIMIT $2 FOR UPDATE SKIP LOCKED`, now, BatchSize)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		var body string
		err := r.Scan(&x.id, &x.entry, &body, &x.tries)
		x.body = json.RawMessage(body)
		return x, err
	})
}

// apply writes what deliver found, and reports how many were kept. A row deliver
// did not reach stays due for the next run.
func apply(ctx context.Context, tx pgx.Tx, rows []row, outcomes map[int64]outcome, now time.Time) (int, error) {
	kept := 0
	for _, r := range rows {
		o, reached := outcomes[r.id]
		var err error
		switch {
		case !reached:
			// Pushed on, so this run's loop does not meet it again.
			_, err = tx.Exec(ctx, `UPDATE audit_outbox SET next_try_at = $2 WHERE id = $1`, r.id, now.Add(time.Minute))
		case o.kept:
			_, err = tx.Exec(ctx, `DELETE FROM audit_outbox WHERE id = $1`, r.id)
			kept++
		case o.refusal != "":
			_, err = tx.Exec(ctx, `UPDATE audit_outbox SET refused_at = $2, refusal = $3, attempts = attempts + 1 WHERE id = $1`, r.id, now, o.refusal)
		default:
			_, err = tx.Exec(ctx, `UPDATE audit_outbox SET attempts = attempts + 1, next_try_at = $2 WHERE id = $1`, r.id, now.Add(o.wait))
		}
		if err != nil {
			return kept, err
		}
	}
	return kept, nil
}

// Outbox sends entries straight after each change (B 13), and from the job PARADOX
// calls for what could not go then. There is no timer (AGENTS C6).
type Outbox struct {
	Pool   *pgxpool.Pool
	Sender Sender
	Logger *slog.Logger

	running atomic.Bool
	again   atomic.Bool
	wg      sync.WaitGroup
}

// Job is the work of audit-catch-up: one drain in the run's transaction.
func (o *Outbox) Job(ctx context.Context, tx pgx.Tx, _ string) (int, error) {
	return Drain(ctx, tx, o.Sender, o.Logger, time.Now())
}

// Kick tries to send what is due, without holding up the person's request. One
// drain runs at a time; a kick during one makes it look again when it ends. If it
// cannot send, the entries wait for the job.
func (o *Outbox) Kick() {
	o.again.Store(true)
	if !o.running.CompareAndSwap(false, true) {
		return
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		for o.again.Swap(false) {
			ctx, cancel := context.WithTimeout(context.Background(), KickFor)
			err := pgx.BeginFunc(ctx, o.Pool, func(tx pgx.Tx) error {
				_, err := Drain(ctx, tx, o.Sender, o.Logger, time.Now())
				return err
			})
			cancel()
			if err != nil && o.Logger != nil {
				o.Logger.Warn("audit entries wait for the catch-up job", "error", err)
			}
		}
		o.running.Store(false)
		if o.again.Load() {
			o.Kick()
		}
	}()
}

// Wait lets a drain in progress end, at shutdown and in tests.
func (o *Outbox) Wait() { o.wg.Wait() }
