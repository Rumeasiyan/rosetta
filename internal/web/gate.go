package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/jackc/pgx/v5"
)

func (a *App) mountGate(mux *http.ServeMux) {
	mux.Handle("GET /gate", a.signedIn(a.only(isAdmin, a.gateScreen)))
	mux.Handle("POST /gate/{source}/{from}/{to}/open", a.signedIn(a.only(isAdmin, a.post(a.openGate))))
	mux.Handle("POST /gate/{source}/{from}/{to}/close", a.signedIn(a.only(isAdmin, a.post(a.closeGate))))
}

// gateRow is one direction for one source on the screen.
type gateRow struct {
	Direction sources.Direction
	Source    bumblebee.Source
	Open      bool
	Gate      sources.Gate
	// NewVersion says an open decision is on an older version, so it starts closed.
	NewVersion bool
}

// gateData is the gate screen's data.
type gateData struct {
	Rows    []gateRow
	History []sources.GateRow
	Today   string
}

// gateRows is every direction every source on the list offers. Whether the source is
// on or resting does not matter here: the gate is about the evaluation.
func gateRows(l bumblebee.List, p sources.Policy) []gateRow {
	var out []gateRow
	for _, d := range sources.Directions() {
		for _, s := range (sources.Policy{Orders: p.Orders}).Ranked(l, d) {
			g, known := p.Gates[sources.GateKey{Source: s.Name, From: d.From, To: d.To}]
			out = append(out, gateRow{Direction: d, Source: s, Open: p.Open(s, d.From, d.To), Gate: g,
				NewVersion: known && g.State == sources.GateOpen && g.SourceVersion != s.Version})
		}
	}
	return out
}

func (a *App) gateScreen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	policy, err := sources.LoadPolicy(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	history, err := sources.GateHistory(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	today, err := people.Today(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	snap, _ := a.snapshot(ctx)
	p := a.page(r, "gate", "gate")
	p.Data = gateData{Rows: gateRows(snap.List, policy), History: history, Today: today.Format(time.DateOnly)}
	a.Render(w, r, http.StatusOK, "gate", p)
}

// gateDirection reads the direction from the address: translation only.
func gateDirection(r *http.Request) (sources.Direction, bool) {
	d, err := sources.ParseDirection("translate", r.PathValue("from"), r.PathValue("to"))
	return d, err == nil
}

func gateRef(name string, d sources.Direction) string { return name + " " + d.String() }

// openGate records the evaluation and opens the direction, with its audit entry, in
// one transaction (R-GAT-01). In force within 60 seconds: R-ADM-05.
func (a *App) openGate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	d, ok := gateDirection(r)
	if !ok {
		a.notFound(w, r)
		return
	}
	name := r.PathValue("source")
	f := r.PostForm
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		today, err := people.Today(ctx, tx)
		if err != nil {
			return err
		}
		e, err := sources.ParseEvaluation(f.Get("test_set"), f.Get("score"), f.Get("evaluated_on"), today)
		if err != nil {
			return err
		}
		if e.Reason, err = sources.ParseReason(f.Get("reason")); err != nil {
			return err
		}
		e.Checked = f.Get("native_checked") != ""
		details := map[string]any{"state": sources.GateOpen, "reason": e.Reason, "test_set_checked": e.Checked, "test_set": e.TestSet, "score": strconv.FormatFloat(e.Score, 'f', -1, 64), "evaluated_on": e.EvaluatedOn.Format(time.DateOnly)}
		// A test set's name or a reason that PENSIEVE would refuse is refused here.
		if audit.CheckDetails(details) != nil {
			return sources.ErrBadEvaluation
		}
		version, err := sources.OpenGate(ctx, tx, name, d, e, c.sess.personID)
		if err != nil {
			return err
		}
		details["source_version"] = version
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionChange, ObjectType: "gate",
			ObjectRef: gateRef(name, d), Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: details}, now)
	})
	a.afterGate(w, r, err, "gateOpened")
}

func (a *App) closeGate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	d, ok := gateDirection(r)
	if !ok {
		a.notFound(w, r)
		return
	}
	name := r.PathValue("source")
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		version, err := sources.CloseGate(ctx, tx, name, d, c.sess.personID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionChange, ObjectType: "gate",
			ObjectRef: gateRef(name, d), Result: audit.ResultDone, Visitor: audit.Visitor(r),
			Details: map[string]any{"state": sources.GateClosed, "source_version": version}}, now)
	})
	a.afterGate(w, r, err, "gateClosed")
}

func (a *App) afterGate(w http.ResponseWriter, r *http.Request, err error, done string) {
	switch {
	case err == nil:
		back(w, r, "/gate", done, "good")
	case errors.Is(err, sources.ErrBadEvaluation):
		back(w, r, "/gate", "gateBad", "bad")
	case errors.Is(err, sources.ErrUnknownSource):
		back(w, r, "/gate", "sourceUnknown", "bad")
	default:
		a.fail(w, r, err)
	}
}
