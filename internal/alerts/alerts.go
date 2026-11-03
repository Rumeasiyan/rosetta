// Package alerts tells the administrator, and a system's owner about their own system, when a state
// changes (R-ADM-03; B 14): by email through HEDWIG, from the state BUMBLEBEE publishes and from
// ROSETTA's own records and flags. One alert goes when a state changes, never one per failed
// request. Silence from audit is PENSIEVE's to flag and is not alerted here.
//
// A notice holds a kind and a reference, never a name or a word of anyone's text. What could not go
// waits as a row in alert, and the job audit-catch-up sends it.
package alerts

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/hedwig"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The kinds (Annex A: alert; R-ADM-03). BumblebeeBack goes beyond R-ADM-03, which names only its
// stopping: a person told of the one needs to hear of the other.
const (
	BumblebeeDown   = "bumblebee not answering"
	BumblebeeBack   = "bumblebee answering"
	SourcePaused    = "source paused"
	SourceResumed   = "source resumed"
	NoSource        = "no source"
	FailuresOver    = "failures over"
	CallerFlagged   = "caller flagged"
	LanguageFlagged = "language flagged"
	PersonFlagged   = "person flagged"
)

// Kinds is every kind sent here; the glossary's notices have their own sender.
var Kinds = []string{BumblebeeDown, BumblebeeBack, SourcePaused, SourceResumed, NoSource, FailuresOver, CallerFlagged, LanguageFlagged, PersonFlagged}

// templates is each kind's notice type in HEDWIG, a template in three languages made at E-ENB-13.
var templates = map[string]string{
	BumblebeeDown: "rosetta_bumblebee_down", BumblebeeBack: "rosetta_bumblebee_back", SourcePaused: "rosetta_source_paused", SourceResumed: "rosetta_source_resumed",
	NoSource: "rosetta_no_source", FailuresOver: "rosetta_failures_over", CallerFlagged: "rosetta_system_flagged", LanguageFlagged: "rosetta_language_flagged", PersonFlagged: "rosetta_person_flagged",
}

// keptDays is how long an alert is kept (Annex A), and past it nobody is told.
const keptDays = 90

// sendWait is the longest one pass of sending may take after a change.
const sendWait = 30 * time.Second

// Querier is what these run through: a pool or a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Alert is one to raise.
type Alert struct {
	Kind     string
	CallerID int64  // the system it is about, or 0
	SourceID int64  // the source it is about, or 0
	Subject  string // a direction, a language or a person's reference
}

func nullable(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// Raise writes an alert, due to go once its transaction is kept.
func Raise(ctx context.Context, q Querier, a Alert) error {
	_, err := q.Exec(ctx, `INSERT INTO alert (kind, caller_id, source_id, subject) VALUES ($1, $2, $3, NULLIF($4, ''))`, a.Kind, nullable(a.CallerID), nullable(a.SourceID), a.Subject)
	return err
}

// Move keeps a state and says whether it changed, and from what ("" for never seen). It is the one
// way a state moves, so one change is one alert.
func Move(ctx context.Context, q Querier, key, value string) (old string, changed bool, err error) {
	err = q.QueryRow(ctx, `SELECT value FROM alert_state WHERE key = $1`, key).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	if err == nil && old == value {
		return old, false, nil
	}
	_, err = q.Exec(ctx, `INSERT INTO alert_state (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, changed_at = now()`, key, value)
	return old, err == nil, err
}

// Sender sends one notice.
type Sender interface {
	Send(ctx context.Context, n hedwig.Notice) (hedwig.Accepted, error)
}

// Notifier sends what is due. After a change a goroutine sends it, never a timer (AGENTS C6), and
// what could not go waits for the job audit-catch-up. Each notice carries its alert's own request
// key, so sending it twice tells nobody twice.
type Notifier struct {
	Pool      *pgxpool.Pool
	Sender    Sender // nil sends nothing: alerts wait
	PublicURL string
	Logger    *slog.Logger

	mu      sync.Mutex
	running bool
	again   bool
	wg      sync.WaitGroup
}

func (n *Notifier) log() *slog.Logger {
	if n.Logger != nil {
		return n.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Kick starts sending what is due, without waiting for it.
func (n *Notifier) Kick() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running {
		n.again = true
		return
	}
	n.running = true
	n.wg.Go(func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), sendWait)
			if _, err := n.Send(ctx, n.Pool); err != nil {
				n.log().Error("alerts were not read")
			}
			cancel()
			n.mu.Lock()
			if !n.again {
				n.running = false
				n.mu.Unlock()
				return
			}
			n.again = false
			n.mu.Unlock()
		}
	})
}

// Wait is for shutting down: it lets a pass in hand finish.
func (n *Notifier) Wait() { n.wg.Wait() }

// Job sends what waits, as part of the job PARADOX calls for what is owed to other services.
func (n *Notifier) Job(ctx context.Context, tx pgx.Tx, _ string) (int, error) { return n.Send(ctx, tx) }

type due struct {
	id             int64
	kind, subject  string
	caller, source *int64
}

// Send goes through what is due and reports how many went. A notice that cannot go stays for the
// next try and stops none of the others; when HEDWIG asks for a wait, the rest wait too.
func (n *Notifier) Send(ctx context.Context, q Querier) (int, error) {
	if n.Sender == nil {
		return 0, nil
	}
	rows, err := q.Query(ctx, `SELECT id, kind, COALESCE(subject, ''), caller_id, source_id FROM alert
		WHERE sent_at IS NULL AND kind = ANY($1) AND raised_at > now() - make_interval(days => $2) ORDER BY id`, Kinds, keptDays)
	if err != nil {
		return 0, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
		var d due
		err := r.Scan(&d.id, &d.kind, &d.subject, &d.caller, &d.source)
		return d, err
	})
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, d := range list {
		notice, err := n.build(ctx, q, d)
		if err != nil {
			return sent, err
		}
		if len(notice.People) == 0 {
			// Nobody to tell: it shows on the status screen, and is not retried for ever.
			n.log().Warn("an alert had nobody to go to", "alert", d.id, "kind", d.kind)
			if _, err := q.Exec(ctx, `UPDATE alert SET sent_at = now() WHERE id = $1`, d.id); err != nil {
				return sent, err
			}
			continue
		}
		accepted, err := n.Sender.Send(ctx, notice)
		if err != nil {
			n.log().Warn("an alert did not go", "alert", d.id, "why", err.Error())
			var refused *hedwig.Refused
			if errors.As(err, &refused) && refused.Wait > 0 {
				break
			}
			continue
		}
		if _, err := q.Exec(ctx, `UPDATE alert SET sent_at = now(), hedwig_notice_id = $2 WHERE id = $1`, d.id, strconv.FormatInt(accepted.RequestID, 10)); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// build is the notice for one alert: the live administrators, and the owner of the system it is
// about (R-ADM-03), each in the language they chose, with the kind, a reference and a link.
func (n *Notifier) build(ctx context.Context, q Querier, d due) (hedwig.Notice, error) {
	out := hedwig.Notice{Type: templates[d.kind], RequestKey: "rosetta-alert-" + strconv.FormatInt(d.id, 10),
		Details: map[string]string{"link": strings.TrimRight(n.PublicURL, "/") + "/status"}}
	if d.subject != "" {
		out.Details["about"] = d.subject
	}
	if d.source != nil {
		var name string
		if err := q.QueryRow(ctx, `SELECT name FROM source WHERE id = $1`, *d.source).Scan(&name); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		out.Details["source"] = name
	}
	var owner string
	if d.caller != nil {
		var reference string
		if err := q.QueryRow(ctx, `SELECT tva_reference, COALESCE(owner_subject, '') FROM caller WHERE id = $1`, *d.caller).Scan(&reference, &owner); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		out.Details["system"] = reference
		out.Details["link"] = strings.TrimRight(n.PublicURL, "/") + "/systems/" + strconv.FormatInt(*d.caller, 10)
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT p.id, p.email, p.screen_language FROM person p LEFT JOIN role_grant g ON g.person_id = p.id
		WHERE p.email <> '' AND ((g.role = 'administrator' AND g.ended_at IS NULL AND g.starts_at <= now()
			AND (g.ends_on IS NULL OR g.ends_on >= (now() AT TIME ZONE 'Asia/Colombo')::date)) OR ($1 <> '' AND p.mjolnir_subject = $1)) ORDER BY p.id`, owner)
	if err != nil {
		return out, err
	}
	out.People, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (hedwig.Person, error) {
		var id int64
		var p hedwig.Person
		err := r.Scan(&id, &p.Email, &p.Language)
		p.Ref = "rosetta-person-" + strconv.FormatInt(id, 10)
		return p, err
	})
	return out, err
}

// Recent is the latest alerts, for the status screen.
type Recent struct {
	Kind     string
	Subject  string
	Source   string
	System   string
	RaisedAt time.Time
	SentAt   *time.Time
}

// Latest reads the latest alerts of these kinds.
func Latest(ctx context.Context, q Querier, limit int) ([]Recent, error) {
	rows, err := q.Query(ctx, `SELECT a.kind, COALESCE(a.subject, ''), COALESCE(s.name, ''), COALESCE(c.name, ''), a.raised_at, a.sent_at FROM alert a
		LEFT JOIN source s ON s.id = a.source_id LEFT JOIN caller c ON c.id = a.caller_id
		WHERE a.kind = ANY($1) ORDER BY a.id DESC LIMIT $2`, Kinds, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Recent, error) {
		var x Recent
		err := r.Scan(&x.Kind, &x.Subject, &x.Source, &x.System, &x.RaisedAt, &x.SentAt)
		return x, err
	})
}
