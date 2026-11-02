// Package settings holds the Office's settings: the values the PRD marks
// "(default: ...)", kept in the setting table (Annex A; R-ADM-05). A setting with
// no row has its PRD default. Stories add theirs here as they need it; a figure
// another system owns is cited there, never copied here.
//
// Every change is kept as history with who made it and when, and is in force
// within 60 seconds with no timer (AGENTS C6): see Cached.
package settings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// Int is a whole-number setting with its PRD default and the values it may take.
type Int struct {
	Name     string
	Default  int
	Min, Max int
	Word     string // message key of its name
	Unit     string // message key of its unit
	Source   string // the requirement that sets it
}

// Every setting so far. Stories read theirs as they come. The range is the sensible
// span for each; only the default is the PRD's.
var (
	RequestLimitSeconds = Int{Name: "request_limit_seconds", Default: 60, Min: 5, Max: 600, Word: "settingRequestLimit", Unit: "unitSeconds", Source: "R-ERR-03"}
	CallerIdleDays      = Int{Name: "caller_idle_days", Default: 90, Min: 1, Max: 3650, Word: "settingCallerIdle", Unit: "unitDays", Source: "R-CAL-05"}
	FailureAlertPercent = Int{Name: "failure_alert_percent", Default: 20, Min: 1, Max: 100, Word: "settingFailureAlert", Unit: "unitPercent", Source: "R-ADM-03"}

	SourceListSeconds = Int{Name: "source_list_seconds", Default: 60, Min: 10, Max: 600, Word: "settingSourceList", Unit: "unitSeconds", Source: "R-PRV-01"}
	CallerPerMinute   = Int{Name: "caller_requests_per_minute", Default: 60, Min: 1, Max: 6000, Word: "settingCallerPerMinute", Unit: "unitRequests", Source: "R-CAL-07"}
	CallerAtOnce      = Int{Name: "caller_requests_at_once", Default: 5, Min: 1, Max: 100, Word: "settingCallerAtOnce", Unit: "unitRequests", Source: "R-CAL-07"}
	PassagesMax       = Int{Name: "passages_max", Default: 200, Min: 1, Max: 1000, Word: "settingPassagesMax", Unit: "unitPassages", Source: "R-TRN-01"}
	PassageCharsMax   = Int{Name: "passage_chars_max", Default: 5000, Min: 1, Max: 50000, Word: "settingPassageChars", Unit: "unitChars", Source: "R-TRN-01"}
	RequestCharsMax   = Int{Name: "request_chars_max", Default: 20000, Min: 1, Max: 200000, Word: "settingRequestChars", Unit: "unitChars", Source: "R-TRN-01"}
	CacheDays         = Int{Name: "cache_days", Default: 30, Min: 1, Max: 365, Word: "settingCacheDays", Unit: "unitDays", Source: "R-TRN-08"}

	All = []Int{RequestLimitSeconds, CallerIdleDays, FailureAlertPercent, SourceListSeconds, CallerPerMinute, CallerAtOnce, PassagesMax, PassageCharsMax, RequestCharsMax, CacheDays}
)

// Find is the setting with this name.
func Find(name string) (Int, bool) {
	for _, s := range All {
		if s.Name == name {
			return s, true
		}
	}
	return Int{}, false
}

// ErrBadValue says a value is not one this setting may take. It never holds the value.
var ErrBadValue = errors.New("settings: not a value this setting may take")

// Parse checks one value, and gives it in one written form only.
func (s Int) Parse(v string) (int, error) {
	v = strings.TrimSpace(v)
	n, err := strconv.Atoi(v)
	if err != nil || n < s.Min || n > s.Max || strings.HasPrefix(v, "+") {
		return 0, ErrBadValue
	}
	return n, nil
}

// Values is every setting's value, by name.
type Values map[string]int

// Of is one setting's value here, or its default.
func (v Values) Of(s Int) int {
	if n, ok := v[s.Name]; ok {
		return n
	}
	return s.Default
}

// Defaults is every setting at its PRD default.
func Defaults() Values {
	out := Values{}
	for _, s := range All {
		out[s.Name] = s.Default
	}
	return out
}

// Querier is what settings are read through.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Load reads every setting. A row that is not a valid value is an error naming the
// setting, never quietly replaced by the default, so a wrong value is seen.
func Load(ctx context.Context, q Querier) (Values, error) {
	rows, err := q.Query(ctx, "SELECT name, value FROM setting")
	if err != nil {
		return nil, err
	}
	type row struct{ name, value string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.name, &x.value)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	out := Defaults()
	for _, x := range list {
		s, known := Find(x.name)
		if !known {
			continue // from a later version: a rollback must not stop the service
		}
		n, err := s.Parse(x.value)
		if err != nil {
			return nil, fmt.Errorf("setting %s holds a value it may not take", s.Name)
		}
		out[s.Name] = n
	}
	return out, nil
}

// Change sets a value in tx, with its history row. It reports the old and new
// values, and whether anything changed; the same value again changes nothing.
func Change(ctx context.Context, tx pgx.Tx, s Int, raw string, by int64) (oldValue, now int, changed bool, err error) {
	if now, err = s.Parse(raw); err != nil {
		return 0, 0, false, err
	}
	// Written with its row locked, so two people changing one setting at once leave
	// history that follows from one to the next.
	if _, err = tx.Exec(ctx, `INSERT INTO setting (name, value, updated_by) VALUES ($1, $2, $3)
		ON CONFLICT (name) DO NOTHING`, s.Name, strconv.Itoa(s.Default), by); err != nil {
		return 0, 0, false, err
	}
	var current string
	if err = tx.QueryRow(ctx, `SELECT value FROM setting WHERE name = $1 FOR UPDATE`, s.Name).Scan(&current); err != nil {
		return 0, 0, false, err
	}
	old, parseErr := s.Parse(current)
	if parseErr != nil {
		old = s.Default // a wrong row is being put right
	}
	if parseErr == nil && old == now {
		return old, now, false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE setting SET value = $2, updated_at = now(), updated_by = $3 WHERE name = $1`,
		s.Name, strconv.Itoa(now), by); err != nil {
		return 0, 0, false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_history (subject, subject_ref, old_value, new_value, changed_by)
		VALUES ('setting', $1, $2, $3, $4)`, s.Name, strconv.Itoa(old), strconv.Itoa(now), by)
	return old, now, err == nil, err
}

// HistoryRow is one change.
type HistoryRow struct {
	Subject   string
	Name      string // the setting's name
	Old, New  string
	ChangedAt time.Time
	By        string
}

// HistoryLimit is the most rows the screen shows.
const HistoryLimit = 200

// History is the latest changes to settings (R-ADM-05).
func History(ctx context.Context, q Querier) ([]HistoryRow, error) {
	rows, err := q.Query(ctx, `SELECT h.subject, h.subject_ref, COALESCE(h.old_value, ''), COALESCE(h.new_value, ''), h.changed_at, p.name
		FROM change_history h JOIN person p ON p.id = h.changed_by
		WHERE h.subject = 'setting' ORDER BY h.id DESC LIMIT $1`, HistoryLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (HistoryRow, error) {
		var x HistoryRow
		err := r.Scan(&x.Subject, &x.Name, &x.Old, &x.New, &x.ChangedAt, &x.By)
		return x, err
	})
}

// RefreshEvery is how often Cached reads again, so a change is in force within 60
// seconds (R-ADM-05) on the first use after this has passed.
const RefreshEvery = 30 * time.Second

// Cached keeps a value read from the settings and reads it again, on use, once it
// is older than Every; there is no timer. A read that fails is logged and the last
// good value kept, so one wrong row never stops ROSETTA. Safe for concurrent use;
// one caller reads while the others go on with the value they have.
type Cached[T any] struct {
	Load   func(ctx context.Context) (T, error)
	Every  time.Duration
	Logger *slog.Logger
	Now    func() time.Time // time.Now when nil

	mu      sync.Mutex
	val     T
	at      time.Time
	loading bool
}

// NewCached starts from first (normally the defaults) and reads on first use.
func NewCached[T any](first T, load func(ctx context.Context) (T, error), every time.Duration, logger *slog.Logger) *Cached[T] {
	return &Cached[T]{Load: load, Every: every, Logger: logger, val: first}
}

// Get is the current value, read again first when it is stale.
func (c *Cached[T]) Get(ctx context.Context) T {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	c.mu.Lock()
	if c.loading || (!c.at.IsZero() && now().Sub(c.at) < c.Every) {
		v := c.val
		c.mu.Unlock()
		return v
	}
	c.loading = true
	c.mu.Unlock()

	v, err := c.Load(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.loading = false
	c.at = now() // a failed read waits a full Every before the next try
	if err != nil {
		if c.Logger != nil {
			c.Logger.Error("settings not read; the last good values stay", "error", err)
		}
		return c.val
	}
	c.val = v
	return v
}
