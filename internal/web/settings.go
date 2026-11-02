package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/jackc/pgx/v5"
)

func (a *App) mountSettings(mux *http.ServeMux) {
	mux.Handle("GET /settings", a.signedIn(a.only(isAdmin, a.settingsPage)))
	mux.Handle("GET /settings/history", a.signedIn(a.only(isAdmin, a.settingsHistory)))
	mux.Handle("POST /settings/{name}", a.signedIn(a.only(isAdmin, a.post(a.settingChange))))
}

// settingRow is one setting on the screen. Saved and InForce differ for at most 60
// seconds after a change (R-ADM-05).
type settingRow struct {
	settings.Int
	Saved, InForce int
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	saved, err := settings.Load(r.Context(), a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	inForce := a.settings.Get(r.Context())
	rows := make([]settingRow, 0, len(settings.All))
	for _, s := range settings.All {
		rows = append(rows, settingRow{Int: s, Saved: saved.Of(s), InForce: inForce.Of(s)})
	}
	p := a.page(r, "settings", "settings")
	p.Data = rows
	a.Render(w, r, http.StatusOK, "settings", p)
}

func (a *App) settingsHistory(w http.ResponseWriter, r *http.Request) {
	rows, err := settings.History(r.Context(), a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	shown := make([]historyRow, 0, len(rows))
	for _, x := range rows {
		h := historyRow{HistoryRow: x}
		if s, ok := settings.Find(x.Name); ok {
			h.Word, h.Unit = s.Word, s.Unit
		}
		shown = append(shown, h)
	}
	p := a.page(r, "settingsHistory", "settings")
	p.Data = shown
	a.Render(w, r, http.StatusOK, "settings-history", p)
}

// historyRow is one change on the screen; a setting no longer known shows its name.
type historyRow struct {
	settings.HistoryRow
	Word, Unit string
}

// settingChange keeps a value, its history and its audit entry in one transaction.
// A refused value is never repeated, kept or logged.
func (a *App) settingChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	s, known := settings.Find(r.PathValue("name"))
	if !known {
		a.notFound(w, r)
		return
	}
	changed := false
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		old, value, did, err := settings.Change(ctx, tx, s, r.PostForm.Get("value"), c.sess.personID)
		if err != nil || !did {
			return err
		}
		changed = true
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionSettingChange, ObjectType: "setting",
			ObjectRef: s.Name, Result: audit.ResultDone, Visitor: audit.Visitor(r),
			Details: map[string]any{"old": strconv.Itoa(old), "new": strconv.Itoa(value)}}, now)
	})
	switch {
	case errors.Is(err, settings.ErrBadValue):
		back(w, r, "/settings", "settingBad", "bad")
	case err != nil:
		a.fail(w, r, err)
	case changed:
		back(w, r, "/settings", "settingSaved", "good")
	default:
		back(w, r, "/settings", "settingUnchanged", "warn")
	}
}
