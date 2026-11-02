// Package flags is the daily work of PARADOX's job "daily" that holds a transaction (R-CAL-02,
// R-CAL-05; s2; B 11): the copies from TVA's file are refreshed, each calling system's flag is worked
// out again, and roles past their end date are ended with an audit entry. The post look-ups that go
// with it wait on another service, so they run after this is kept (internal/posts).
package flags

import (
	"context"
	"log/slog"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/tva"
	"github.com/jackc/pgx/v5"
)

// readTimeout is the longest the job waits on TVA's file.
const readTimeout = 10 * time.Second

// Changes is what a day's work moved, for whoever tells people.
type Changes struct {
	// Systems is each system whose flag moved.
	Systems []SystemChange
	// Languages is each language with no live glossary keeper now.
	Languages []string
}

// SystemChange is one system's flag moving.
type SystemChange struct {
	ID       int64
	Old, New string
}

// Daily holds what the day's work needs.
type Daily struct {
	TVA      tva.Reader
	Settings func(ctx context.Context) settings.Values
	Logger   *slog.Logger
	// Told, when set, hears what moved, in the same transaction.
	Told func(ctx context.Context, tx pgx.Tx, c Changes) error
}

// flagOf is the one flag a system carries, the most pressing first: a system not to be called, then
// one nobody answers for, then one gone quiet (Annex A has one column).
func flagOf(status string, inFile bool, subject string, lastSeen time.Time, idleDays int, now time.Time) string {
	switch {
	case !inFile || !tva.MayCall(status):
		return callers.FlagNotLive
	case subject == "":
		return callers.FlagNoOwner
	case now.Sub(lastSeen) > time.Duration(idleDays)*24*time.Hour:
		return callers.FlagIdle
	}
	return callers.FlagNone
}

// Run is the job's work. A file that cannot be read, or holds no system at all, changes no copy and
// is logged, so a damaged file never reads as every system gone; flags are still worked out from
// what is held. It reports how many systems it looked at.
func (d *Daily) Run(ctx context.Context, tx pgx.Tx, _ string) (int, error) {
	logger := d.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	var file tva.File
	haveFile := false
	if d.TVA != nil {
		rctx, cancel := context.WithTimeout(ctx, readTimeout)
		f, err := d.TVA.Read(rctx)
		cancel()
		switch {
		case err != nil:
			logger.Error("the system register's file was not read; the copies stay", "error", err)
		case len(f.Systems) == 0:
			logger.Error("the system register's file holds no system; the copies stay")
		default:
			file, haveFile = f, true
		}
	}
	systems, err := callers.Refreshable(ctx, tx)
	if err != nil {
		return 0, err
	}
	idle := d.Settings(ctx).Of(settings.CallerIdleDays)
	now := time.Now()
	var changes Changes
	for _, s := range systems {
		inFile := true
		status, subject := s.TVAStatus, s.OwnerSubject
		if haveFile {
			var found tva.System
			found, inFile = file.Find(s.Reference)
			if inFile {
				if err := callers.Refresh(ctx, tx, s.ID, found); err != nil {
					return 0, err
				}
				status = found.Status
				subject, _ = found.Owner()
			}
		}
		last := s.RegisteredAt
		if s.LastRequestAt != nil {
			last = *s.LastRequestAt
		}
		flag := flagOf(status, inFile, subject, last, idle, now)
		if flag != s.Flag {
			if err := callers.SetFlag(ctx, tx, s.ID, flag); err != nil {
				return 0, err
			}
			changes.Systems = append(changes.Systems, SystemChange{ID: s.ID, Old: s.Flag, New: flag})
		}
	}
	ended, err := people.EndPastDate(ctx, tx)
	if err != nil {
		return 0, err
	}
	for _, g := range ended {
		details := map[string]any{"change": "ended", "role": g.Role}
		if g.Language != "" {
			details["language"] = string(g.Language)
		}
		if err := audit.Record(ctx, tx, audit.Event{Actor: audit.System, Action: audit.ActionRoleChange, ObjectType: "person", ObjectRef: g.Subject,
			Result: audit.ResultDone, Reason: "The role's end date has passed", Details: details}, now); err != nil {
			return 0, err
		}
	}
	if changes.Languages, err = people.LanguagesWithoutKeeper(ctx, tx); err != nil {
		return 0, err
	}
	if d.Told != nil {
		if err := d.Told(ctx, tx, changes); err != nil {
			return 0, err
		}
	}
	logger.Info("daily flags", "systems", len(systems), "flags_moved", len(changes.Systems), "roles_past_date", len(ended), "languages_without_keeper", len(changes.Languages), "register_read", haveFile)
	return len(systems), nil
}
