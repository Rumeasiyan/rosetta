package web

import (
	"net/http"

	"github.com/Rumeasiyan/rosetta/internal/alerts"
	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/usage"
)

// statusAlerts is how many of the latest alerts the screen shows.
const statusAlerts = 20

func alertWord(kind string) string {
	switch kind {
	case alerts.BumblebeeDown:
		return "alertBumblebeeDown"
	case alerts.BumblebeeBack:
		return "alertBumblebeeBack"
	case alerts.SourcePaused:
		return "alertSourcePaused"
	case alerts.SourceResumed:
		return "alertSourceResumed"
	case alerts.NoSource:
		return "alertNoSource"
	case alerts.FailuresOver:
		return "alertFailuresOver"
	case alerts.CallerFlagged:
		return "alertCallerFlagged"
	case alerts.LanguageFlagged:
		return "alertLanguageFlagged"
	}
	return "alertPersonFlagged"
}

func (a *App) mountStatus(mux *http.ServeMux) {
	mux.Handle("GET /status", a.signedIn(a.only(seesSystems, a.statusPage)))
}

// statusReason is one reason on the screen, with how many today.
type statusReason struct {
	Reason  string
	Failure bool
	Count   int64
}

// statusData is what R-ADM-02 asks for. A system owner sees BUMBLEBEE and the sources, which are
// nobody's in particular, and today's counts for their own systems (R-ADM-01).
type statusData struct {
	Up       bool
	ReadAt   any
	Sources  []bumblebee.Source
	Requests int64
	Refusals int64
	Failures int64
	Reasons  []statusReason
	Admin    bool
	NoSystem []statusReason
	// What has not reached PENSIEVE, which only a screen and the log can show (the decision on the
	// audit outbox); silence from audit is PENSIEVE's own to flag.
	OutboxWaiting, OutboxRefused int
	// What is flagged to the administrator (R-CAL-02, R-CAL-05, R-ADM-06; s2).
	FlaggedSystems   []callers.Caller
	FlaggedLanguages []string
	FlaggedPeople    []people.PersonToLookUp
	// The latest alerts, and whether each has gone (R-ADM-03).
	Alerts []alerts.Recent
}

// Flagged says anything is.
func (d statusData) Flagged() bool {
	return len(d.FlaggedSystems) > 0 || len(d.FlaggedLanguages) > 0 || len(d.FlaggedPeople) > 0
}

func (a *App) statusPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	snap, _ := a.snapshot(ctx)
	d := statusData{Up: snap.Fresh, Sources: snap.List.Sources, Admin: isAdmin(c)}
	if !snap.ReadAt.IsZero() {
		d.ReadAt = snap.ReadAt
	}
	var own []callers.Caller
	var ids []int64
	var err error
	if !d.Admin {
		if own, err = callers.OwnedBy(ctx, a.deps.Pool, c.sess.subject); err != nil {
			a.fail(w, r, err)
			return
		}
		for _, s := range own {
			ids = append(ids, s.ID)
		}
	}
	today, err := usage.Today(ctx, a.deps.Pool, ids, d.Admin)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	d.Requests = today[usage.Answered]
	for _, reason := range reasons.All {
		n := today[string(reason)]
		d.Requests += n
		if reason.IsFailure() {
			d.Failures += n
		} else {
			d.Refusals += n
		}
		if n > 0 {
			d.Reasons = append(d.Reasons, statusReason{string(reason), reason.IsFailure(), n})
		}
	}
	if d.Admin {
		if a.deps.NoSystem != nil {
			counts := a.deps.NoSystem()
			for _, reason := range reasons.All {
				if n := counts[reason]; n > 0 {
					d.NoSystem = append(d.NoSystem, statusReason{string(reason), reason.IsFailure(), int64(n)})
				}
			}
		}
		if d.OutboxWaiting, d.OutboxRefused, err = audit.Waiting(ctx, a.deps.Pool); err != nil {
			a.fail(w, r, err)
			return
		}
		if d.Alerts, err = alerts.Latest(ctx, a.deps.Pool, statusAlerts); err != nil {
			a.fail(w, r, err)
			return
		}
		if d.FlaggedSystems, err = callers.Flagged(ctx, a.deps.Pool); err != nil {
			a.fail(w, r, err)
			return
		}
		if d.FlaggedLanguages, err = people.LanguagesWithoutKeeper(ctx, a.deps.Pool); err != nil {
			a.fail(w, r, err)
			return
		}
		if d.FlaggedPeople, err = people.FlaggedPeople(ctx, a.deps.Pool); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	p := a.page(r, "statusScreen", "status")
	p.Data = d
	a.Render(w, r, http.StatusOK, "status", p)
}
