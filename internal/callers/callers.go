// Package callers keeps the calling systems (Annex A: caller): a system joins only
// if TVA gives its status as live or may join (R-CAL-01), and its name and owner are
// copies from TVA's file, never edited here (R-CAL-02).
package callers

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/tva"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The flags a system can carry (Annex A; R-CAL-02, R-CAL-05).
const (
	FlagNone    = "none"
	FlagNoOwner = "no owner"
	FlagIdle    = "idle"
	FlagNotLive = "not live or may join"
)

// The refusals registering a system can meet. None holds what was typed.
var (
	ErrUnknownSystem = errors.New("callers: the register has no such reference")
	ErrNotAllowed    = errors.New("callers: the register's status is neither live nor may join")
	ErrAlready       = errors.New("callers: the system is registered already")
	ErrNoSuchSystem  = errors.New("callers: no such calling system")
)

// Querier is what these run through: a pool or a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Caller is one calling system.
type Caller struct {
	ID            int64
	Reference     string
	Name          string
	OwnerPostRef  string // "" when TVA names none
	OwnerSubject  string // "" when vacant or with no linked sign-in (R-CAL-02)
	TVAStatus     string
	TVACheckedAt  time.Time
	PerMinute     int
	AtOnce        int
	RegisteredAt  time.Time
	LastRequestAt *time.Time
	Flag          string
}

// HasOwner reports whether a person answers for the system. If not, the
// administrator acts for it meanwhile (R-CAL-02).
func (c Caller) HasOwner() bool { return c.OwnerSubject != "" }

// FlagFor is the flag a system takes from TVA's file. Matched on the subject alone:
// a null subject is the flag, whatever else the file says of the post.
func FlagFor(s tva.System) string {
	if subject, _ := s.Owner(); subject == "" {
		return FlagNoOwner
	}
	return FlagNone
}

// Choose finds the system to register in TVA's file and checks that it may join.
// reference is compared exactly, as TVA's references are opaque.
func Choose(f tva.File, reference string) (tva.System, error) {
	s, ok := f.Find(reference)
	if !ok {
		return tva.System{}, ErrUnknownSystem
	}
	if !tva.MayCall(s.Status) {
		return tva.System{}, ErrNotAllowed
	}
	return s, nil
}

// fit shortens a name to what the table holds (Annex A: 200), so a long name in
// TVA's file never stops a registration.
func fit(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Register keeps s as a calling system, its status and owner as TVA gives them now,
// with the limits a new system starts with.
func Register(ctx context.Context, tx pgx.Tx, s tva.System, l Limits, by int64) (Caller, error) {
	subject, post := s.Owner()
	row := tx.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, owner_post_ref, owner_subject, tva_status, tva_checked_at, registered_by, flag, requests_per_minute, requests_at_once)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, now(), $6, $7, $8, $9)
		ON CONFLICT (tva_reference) DO NOTHING RETURNING `+columns,
		s.Reference, fit(s.Name(), 200), post, subject, s.Status, by, FlagFor(s), l.PerMinute, l.AtOnce)
	c, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrAlready
	}
	return c, err
}

const columns = `id, tva_reference, name, COALESCE(owner_post_ref, ''), COALESCE(owner_subject, ''), tva_status, tva_checked_at,
	requests_per_minute, requests_at_once, registered_at, last_request_at, flag`

func scan(r pgx.Row) (Caller, error) {
	var c Caller
	err := r.Scan(&c.ID, &c.Reference, &c.Name, &c.OwnerPostRef, &c.OwnerSubject, &c.TVAStatus, &c.TVACheckedAt,
		&c.PerMinute, &c.AtOnce, &c.RegisteredAt, &c.LastRequestAt, &c.Flag)
	return c, err
}

// Get is one calling system.
func Get(ctx context.Context, q Querier, id int64) (Caller, error) {
	c, err := scan(q.QueryRow(ctx, `SELECT `+columns+` FROM caller WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNoSuchSystem
	}
	return c, err
}

func list(ctx context.Context, q Querier, sql string, args ...any) ([]Caller, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Caller, error) { return scan(r) })
}

// All is every calling system, for the administrator.
func All(ctx context.Context, q Querier) ([]Caller, error) {
	return list(ctx, q, `SELECT `+columns+` FROM caller ORDER BY name, id`)
}

// OwnedBy is the systems whose owner is subject: their owner only reaches these
// (R-ADM-01). Matched on the subject, never on email.
func OwnedBy(ctx context.Context, q Querier, subject string) ([]Caller, error) {
	if subject == "" {
		return nil, nil
	}
	return list(ctx, q, `SELECT `+columns+` FROM caller WHERE owner_subject = $1 ORDER BY name, id`, subject)
}

// Limits is a system's limits (R-CAL-07).
type Limits struct {
	PerMinute, AtOnce int
}

// ParseLimits checks what the form sent, against the same ranges as the settings that
// give a new system its limits.
func ParseLimits(perMinute, atOnce string) (Limits, error) {
	m, err := settings.CallerPerMinute.Parse(perMinute)
	if err != nil {
		return Limits{}, err
	}
	o, err := settings.CallerAtOnce.Parse(atOnce)
	if err != nil {
		return Limits{}, err
	}
	return Limits{PerMinute: m, AtOnce: o}, nil
}

// SetLimits changes a system's limits, with its history row. They are read on every
// request, so they are in force at once. It reports whether anything changed.
func SetLimits(ctx context.Context, tx pgx.Tx, s Caller, l Limits, by int64) (Limits, bool, error) {
	var old Limits
	err := tx.QueryRow(ctx, `SELECT requests_per_minute, requests_at_once FROM caller WHERE id = $1 FOR UPDATE`, s.ID).Scan(&old.PerMinute, &old.AtOnce)
	if errors.Is(err, pgx.ErrNoRows) {
		return old, false, ErrNoSuchSystem
	}
	if err != nil || old == l {
		return old, false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE caller SET requests_per_minute = $2, requests_at_once = $3 WHERE id = $1`, s.ID, l.PerMinute, l.AtOnce); err != nil {
		return old, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_history (subject, subject_ref, old_value, new_value, changed_by) VALUES ('caller limit', $1, $2, $3, $4)`,
		s.Reference, old.text(), l.text(), by)
	return old, err == nil, err
}

func (l Limits) text() string { return fmt.Sprintf("%d a minute, %d at once", l.PerMinute, l.AtOnce) }

// LimitChange is one change to a system's limits.
type LimitChange struct {
	Old, New  string
	ChangedAt time.Time
	By        string
}

// limitHistory is the most changes a system's screen shows.
const limitHistory = 20

// LimitHistory is the latest changes to one system's limits.
func LimitHistory(ctx context.Context, q Querier, reference string) ([]LimitChange, error) {
	rows, err := q.Query(ctx, `SELECT COALESCE(h.old_value, ''), COALESCE(h.new_value, ''), h.changed_at, p.name
		FROM change_history h JOIN person p ON p.id = h.changed_by
		WHERE h.subject = 'caller limit' AND h.subject_ref = $1 ORDER BY h.id DESC LIMIT $2`, reference, limitHistory)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LimitChange, error) {
		var x LimitChange
		err := r.Scan(&x.Old, &x.New, &x.ChangedAt, &x.By)
		return x, err
	})
}
