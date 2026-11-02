package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/jackc/pgx/v5"
)

// SourceList is BUMBLEBEE's source list as the screens read it.
type SourceList interface {
	Snapshot(ctx context.Context) sources.Snapshot
	MaxText() int
}

func (a *App) mountSources(mux *http.ServeMux) {
	mux.Handle("GET /sources", a.signedIn(a.only(seesSystems, a.sourcesPage)))
	mux.Handle("GET /sources/history", a.signedIn(a.only(isAdmin, a.sourcesHistory)))
	mux.Handle("POST /sources/order/{op}/{from}/{to}", a.signedIn(a.only(isAdmin, a.post(a.setOrder))))
	mux.Handle("POST /sources/switch/{name}", a.signedIn(a.only(isAdmin, a.post(a.setSwitch))))
}

// stateWord is the message key for a source's state.
func stateWord(state string) string {
	switch state {
	case bumblebee.StateOn:
		return "stateOn"
	case bumblebee.StateRestingBlock:
		return "stateRestBlock"
	case bumblebee.StateRestingChange:
		return "stateRestChange"
	}
	return "stateOff"
}

// sourceRow is one source on the screen. Every source is an outside website, and the
// screen says so beside each, so the Office and system owners can choose (R-NFR-13).
type sourceRow struct {
	bumblebee.Source
	OnForRosetta bool
}

// orderForm is one direction's order, each source with its position or none.
type orderForm struct {
	Direction sources.Direction
	Rows      []orderRow
}

type orderRow struct {
	Name     string
	Position int // 0 when not placed
}

// sourcesData is the source screen's data.
type sourcesData struct {
	Snapshot sources.Snapshot
	MaxText  int
	Rows     []sourceRow
	Orders   []orderForm
	Admin    bool
}

// buildSourcesData joins the list with what the administrator has set.
func buildSourcesData(snap sources.Snapshot, maxText int, p sources.Policy, admin bool) sourcesData {
	d := sourcesData{Snapshot: snap, MaxText: maxText, Admin: admin}
	for _, s := range snap.List.Sources {
		d.Rows = append(d.Rows, sourceRow{Source: s, OnForRosetta: !p.Off[s.Name]})
	}
	// An order form shows every source offering the work, on or off, so a source
	// turned off keeps its place.
	everyOn := sources.Policy{Orders: p.Orders}
	for _, dir := range append(sources.Directions(), sources.Grammar) {
		f := orderForm{Direction: dir}
		for _, s := range everyOn.Ranked(snap.List, dir) {
			f.Rows = append(f.Rows, orderRow{Name: s.Name, Position: slices.Index(p.Orders[dir], s.Name) + 1})
		}
		if len(f.Rows) > 0 {
			d.Orders = append(d.Orders, f)
		}
	}
	return d
}

func (a *App) snapshot(ctx context.Context) (sources.Snapshot, int) {
	if a.deps.Sources == nil {
		return sources.Snapshot{}, sources.DefaultMaxText
	}
	return a.deps.Sources.Snapshot(ctx), a.deps.Sources.MaxText()
}

func (a *App) sourcesPage(w http.ResponseWriter, r *http.Request) {
	policy, err := sources.LoadPolicy(r.Context(), a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	snap, maxText := a.snapshot(r.Context())
	p := a.page(r, "sources", "sources")
	p.Data = buildSourcesData(snap, maxText, policy, isAdmin(fromCtx(r)))
	a.Render(w, r, http.StatusOK, "sources", p)
}

func (a *App) sourcesHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := sources.History(r.Context(), a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "sourcesHistory", "sources")
	p.Data = rows
	a.Render(w, r, http.StatusOK, "sources-history", p)
}

// setOrder keeps one direction's order, its history and its audit entry in one
// transaction. In force within 60 seconds: R-ADM-05.
func (a *App) setOrder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	dir, err := sources.ParseDirection(r.PathValue("op"), r.PathValue("from"), r.PathValue("to"))
	if err != nil {
		a.notFound(w, r)
		return
	}
	snap, _ := a.snapshot(ctx)
	var known []string
	for _, s := range (sources.Policy{}).Ranked(snap.List, dir) {
		known = append(known, s.Name)
	}
	order, err := sources.ParseOrder(known, func(name string) string { return r.PostForm.Get("pos:" + name) })
	if err != nil {
		back(w, r, "/sources", "orderBad", "bad")
		return
	}
	changed := false
	err = a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		before, after, did, err := sources.SetOrder(ctx, tx, dir, order, c.sess.personID)
		if err != nil || !did {
			return err
		}
		changed = true
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionChange, ObjectType: "source order",
			ObjectRef: dir.String(), Result: audit.ResultDone, Visitor: audit.Visitor(r),
			Details: map[string]any{"old": before, "new": after}}, now)
	})
	a.afterSourceChange(w, r, err, changed)
}

// setSwitch turns a source on or off for ROSETTA (R-PRV-02).
func (a *App) setSwitch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	name := r.PathValue("name")
	on, err := strconv.ParseBool(r.PostForm.Get("on"))
	if err != nil {
		back(w, r, "/sources", "orderBad", "bad")
		return
	}
	changed := false
	err = a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		did, err := sources.SetSwitch(ctx, tx, name, on, c.sess.personID)
		if err != nil || !did {
			return err
		}
		changed = true
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionChange, ObjectType: "source",
			ObjectRef: name, Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: map[string]any{"on_for_rosetta": on}}, now)
	})
	a.afterSourceChange(w, r, err, changed)
}

func (a *App) afterSourceChange(w http.ResponseWriter, r *http.Request, err error, changed bool) {
	switch {
	case errors.Is(err, sources.ErrUnknownSource):
		back(w, r, "/sources", "sourceUnknown", "bad")
	case err != nil:
		a.fail(w, r, err)
	case changed:
		back(w, r, "/sources", "settingSaved", "good")
	default:
		back(w, r, "/sources", "settingUnchanged", "warn")
	}
}

// directionName is a direction in a person's language.
func directionName(l i18n.Lang, d sources.Direction) string {
	if d.Operation == bumblebee.OpGrammar {
		return i18n.T(l, "grammarEnglish")
	}
	from, _ := i18n.Parse(d.From)
	to, _ := i18n.Parse(d.To)
	return i18n.Name(from) + " \u2192 " + i18n.Name(to)
}
