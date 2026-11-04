package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/flags"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The states kept in alert_state.
const (
	stateUp   = "up"
	stateDown = "down"
	stateOK   = "ok"
	stateNone = "none"
	stateOver = "over"
	stateFlag = "flagged"
)

// Watcher looks at the published state on each pass of the source list's timer, the one timer
// AGENTS C6 allows, and raises an alert for each state that moved.
type Watcher struct {
	Pool     *pgxpool.Pool
	Snapshot func(ctx context.Context) sources.Snapshot
	Policy   func(ctx context.Context) sources.Policy
	Settings func(ctx context.Context) settings.Values
	Kick     func()
	Logger   *slog.Logger
	Now      func() time.Time // time.Now when nil
	// Hold is the least time between two moves of BUMBLEBEE's state, a source's or a direction's.
	Hold    time.Duration
	started time.Time
}

// Hold is what the service holds a flapping state for: one alert an hour for each, at most.
const Hold = time.Hour

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Watcher) log() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// watchTimeout bounds one look, as the source list's timer waits for it.
const watchTimeout = 10 * time.Second

// Tick is one look. A failure logs and is tried on the next pass, with nothing told twice, as each
// move is kept with its alert in one transaction.
func (w *Watcher) Tick(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, watchTimeout)
	defer cancel()
	if w.started.IsZero() {
		w.started = w.now()
	}
	snap := w.Snapshot(ctx)
	// Starting up with nothing read yet is not BUMBLEBEE stopping: it has its full age to answer in.
	if !snap.Fresh && snap.ReadAt.IsZero() && w.now().Sub(w.started) < time.Duration(w.Settings(ctx).Of(settings.SourceListSeconds))*time.Second {
		return
	}
	raised := 0
	err := pgx.BeginFunc(ctx, w.Pool, func(tx pgx.Tx) error {
		var err error
		if raised, err = w.look(ctx, tx, snap); err != nil {
			return err
		}
		more, err := w.failures(ctx, tx)
		raised += more
		return err
	})
	if err != nil {
		w.log().Error("states were not looked at", "error", err)
		return
	}
	if raised > 0 && w.Kick != nil {
		w.Kick()
	}
}

// look compares BUMBLEBEE, its sources and each direction with what was last alerted on. While the
// list is stale only BUMBLEBEE's state is news: the list says nothing new, so a down BUMBLEBEE is one
// alert and not one for each direction.
func (w *Watcher) look(ctx context.Context, tx pgx.Tx, snap sources.Snapshot) (int, error) {
	raised := 0
	up := stateDown
	if snap.Fresh {
		up = stateUp
	}
	old, changed, err := MoveHeld(ctx, tx, "bumblebee", up, w.Hold)
	if err != nil {
		return 0, err
	}
	if changed {
		switch {
		case up == stateDown:
			err = Raise(ctx, tx, Alert{Kind: BumblebeeDown})
			raised++
		case old == stateDown:
			err = Raise(ctx, tx, Alert{Kind: BumblebeeBack})
			raised++
		}
		if err != nil {
			return 0, err
		}
	}
	if !snap.Fresh {
		return raised, nil
	}
	for _, s := range snap.List.Sources {
		state := s.State
		old, changed, err := MoveHeld(ctx, tx, "source:"+s.Name, state, w.Hold)
		if err != nil {
			return 0, err
		}
		if !changed {
			continue
		}
		var id int64
		if err := tx.QueryRow(ctx, `SELECT id FROM source WHERE name = $1`, s.Name).Scan(&id); err != nil {
			return 0, err
		}
		kind := ""
		switch {
		case state != bumblebee.StateOn && (old == bumblebee.StateOn || old == ""):
			kind = SourcePaused
		case state == bumblebee.StateOn && old != "":
			kind = SourceResumed
		}
		if kind != "" {
			if err := Raise(ctx, tx, Alert{Kind: kind, SourceID: id}); err != nil {
				return 0, err
			}
			raised++
		}
	}
	policy := w.Policy(ctx)
	for _, d := range append(sources.Directions(), sources.Grammar) {
		state := stateOK
		if !AnyAvailable(snap.List, policy, d, w.now()) {
			state = stateNone
		}
		_, changed, err := MoveHeld(ctx, tx, "direction:"+d.String(), state, w.Hold)
		if err != nil {
			return 0, err
		}
		if changed && state == stateNone {
			if err := Raise(ctx, tx, Alert{Kind: NoSource, Subject: d.String()}); err != nil {
				return 0, err
			}
			raised++
		}
	}
	return raised, nil
}

// AnyAvailable says this work has somewhere to go now: a source on for ROSETTA, open under the gate
// for translation, and not resting. A direction never opened has nothing to go wrong: it answers
// "human translation required", which is the gate's own state and no alert (R-GAT-03).
func AnyAvailable(l bumblebee.List, p sources.Policy, d sources.Direction, now time.Time) bool {
	any := false
	for _, s := range p.Ranked(l, d) {
		if d.Operation != bumblebee.OpGrammar && !p.Open(s, d.From, d.To) {
			continue
		}
		any = true
		if s.Usable(now) {
			return true
		}
	}
	return !any
}

// failures compares each system's last hour with the setting (R-ADM-03). Tests from the screens do
// not count, and only failures do: a caller's own refused requests are no fault here (the difference
// reasons.IsFailure draws).
func (w *Watcher) failures(ctx context.Context, tx pgx.Tx) (int, error) {
	var words []string
	for _, r := range reasons.All {
		if r.IsFailure() {
			words = append(words, string(r))
		}
	}
	limit := w.Settings(ctx).Of(settings.FailureAlertPercent)
	rows, err := tx.Query(ctx, `SELECT c.id, (u.failed * 100 > u.total * $2) FROM caller c
		LEFT JOIN (SELECT caller_id, count(*) AS total, count(*) FILTER (WHERE outcome = ANY($1)) AS failed FROM usage_record
			WHERE at > now() - interval '1 hour' AND operation <> 'test' GROUP BY caller_id) u ON u.caller_id = c.id ORDER BY c.id`, words, limit)
	if err != nil {
		return 0, err
	}
	type line struct {
		id   int64
		over *bool
	}
	lines, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (line, error) {
		var x line
		err := r.Scan(&x.id, &x.over)
		return x, err
	})
	if err != nil {
		return 0, err
	}
	raised := 0
	for _, x := range lines {
		state := stateOK
		if x.over != nil && *x.over {
			state = stateOver
		}
		_, changed, err := Move(ctx, tx, fmt.Sprintf("failures:%d", x.id), state)
		if err != nil {
			return 0, err
		}
		if changed && state == stateOver {
			if err := Raise(ctx, tx, Alert{Kind: FailuresOver, CallerID: x.id}); err != nil {
				return 0, err
			}
			raised++
		}
	}
	return raised, nil
}

// Flags tells of what the daily flags moved: a system newly flagged, and a language newly without a
// keeper (R-ADM-03; s2). A system that moves to no flag needs nobody told.
func Flags(ctx context.Context, tx pgx.Tx, c flags.Changes) error {
	for _, s := range c.Systems {
		if s.New != callers.FlagNone {
			if err := Raise(ctx, tx, Alert{Kind: CallerFlagged, CallerID: s.ID, Subject: s.New}); err != nil {
				return err
			}
		}
	}
	without := map[string]bool{}
	for _, l := range c.Languages {
		without[l] = true
	}
	for _, l := range []string{"si", "ta", "en"} {
		state := stateOK
		if without[l] {
			state = stateFlag
		}
		_, changed, err := Move(ctx, tx, "language:"+l, state)
		if err != nil {
			return err
		}
		if changed && state == stateFlag {
			if err := Raise(ctx, tx, Alert{Kind: LanguageFlagged, Subject: l}); err != nil {
				return err
			}
		}
	}
	return nil
}

// PersonState keeps whether a person is flagged, and raises one alert when they become so
// (R-ADM-06). The reference is opaque, and the screen says who.
func PersonState(ctx context.Context, tx pgx.Tx, personID int64, flagged bool) error {
	state := stateOK
	if flagged {
		state = stateFlag
	}
	_, changed, err := Move(ctx, tx, fmt.Sprintf("person:%d", personID), state)
	if err != nil || !changed || !flagged {
		return err
	}
	return Raise(ctx, tx, Alert{Kind: PersonFlagged, Subject: fmt.Sprintf("rosetta-person-%d", personID)})
}
