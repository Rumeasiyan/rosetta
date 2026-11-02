package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/jackc/pgx/v5"
)

// tvaReadTimeout is the longest a registration waits on TVA's file.
const tvaReadTimeout = 10 * time.Second

func (a *App) mountSystems(mux *http.ServeMux) {
	mux.Handle("GET /systems", a.signedIn(a.only(seesSystems, a.systemsPage)))
	mux.Handle("POST /systems/register", a.signedIn(a.only(isAdmin, a.post(a.registerSystem))))
	mux.Handle("GET /systems/{id}", a.signedIn(a.forSystem(a.systemPage)))
}

// seesSystems is who the screen opens for: the administrator, and a system owner
// for their own systems.
func seesSystems(c *current) bool { return isAdmin(c) || len(c.owned) > 0 }

// manages says this person acts for this system: its owner by TVA's file, matched
// on the subject alone (R-CAL-02), or the administrator, who also acts for a
// system with no owner.
func manages(c *current, s callers.Caller) bool {
	return isAdmin(c) || (s.OwnerSubject != "" && c.sess.subject == s.OwnerSubject)
}

// forSystem finds the system in the address and lets only those who manage it on.
// A system that does not exist looks like one they may not reach, to anyone but
// the administrator, so an owner learns nothing of other systems.
func (a *App) forSystem(next func(http.ResponseWriter, *http.Request, callers.Caller)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := fromCtx(r)
		var s callers.Caller
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err == nil {
			s, err = callers.Get(r.Context(), a.deps.Pool, id)
		}
		switch {
		case err != nil && !errors.Is(err, callers.ErrNoSuchSystem) && !errors.Is(err, strconv.ErrSyntax) && !errors.Is(err, strconv.ErrRange):
			a.fail(w, r, err)
		case err != nil && isAdmin(c):
			a.notFound(w, r)
		case err != nil || !manages(c, s):
			a.deny(w, r, "Not this person's system", "notAllowed")
		default:
			next(w, r, s)
		}
	}
}

// flagWord is the message key for a stored flag.
func flagWord(flag string) string {
	switch flag {
	case callers.FlagNoOwner:
		return "flagNoOwner"
	case callers.FlagIdle:
		return "flagIdle"
	case callers.FlagNotLive:
		return "flagNotLive"
	}
	return "flagNone"
}

// systemsData is the list screen's data.
type systemsData struct {
	Systems  []callers.Caller
	Register bool // the form only the administrator has
}

func (a *App) systemsPage(w http.ResponseWriter, r *http.Request) {
	c := fromCtx(r)
	var list []callers.Caller
	var err error
	if isAdmin(c) {
		list, err = callers.All(r.Context(), a.deps.Pool)
	} else {
		list, err = callers.OwnedBy(r.Context(), a.deps.Pool, c.sess.subject)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "systems", "systems")
	p.Data = systemsData{Systems: list, Register: isAdmin(c)}
	a.Render(w, r, http.StatusOK, "systems", p)
}

// systemData is one system's screen.
type systemData struct {
	System callers.Caller
}

func (a *App) systemPage(w http.ResponseWriter, r *http.Request, s callers.Caller) {
	p := a.page(r, "systems", "systems")
	p.Data = systemData{System: s}
	a.Render(w, r, http.StatusOK, "system", p)
}

// registerRefusal is the message for each refusal. What was typed is never repeated.
func registerRefusal(err error) (string, bool) {
	for refusal, word := range map[error]string{
		callers.ErrUnknownSystem: "systemUnknown",
		callers.ErrNotAllowed:    "systemNotAllowed",
		callers.ErrAlready:       "systemAlready",
	} {
		if errors.Is(err, refusal) {
			return word, true
		}
	}
	return "", false
}

// registerSystem reads TVA's file now and keeps what it says (R-CAL-01), with its
// audit entry in the same transaction. The file is read before the transaction, so
// a slow read holds no connection from the pool of five.
func (a *App) registerSystem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	if a.deps.TVA == nil {
		back(w, r, "/systems", "tvaUnreadable", "bad")
		return
	}
	rctx, cancel := context.WithTimeout(ctx, tvaReadTimeout)
	defer cancel()
	file, err := a.deps.TVA.Read(rctx)
	if err != nil {
		a.deps.Logger.Error("the system register's file was not read", "error", err)
		back(w, r, "/systems", "tvaUnreadable", "bad")
		return
	}
	sys, err := callers.Choose(file, strings.TrimSpace(r.PostForm.Get("reference")))
	if err != nil {
		word, _ := registerRefusal(err)
		back(w, r, "/systems", word, "bad")
		return
	}
	var made callers.Caller
	err = a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		var err error
		if made, err = callers.Register(ctx, tx, sys, c.sess.personID); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionCreation, ObjectType: "calling system",
			ObjectRef: sys.Reference, Result: audit.ResultDone, Visitor: audit.Visitor(r),
			Details: map[string]any{"change": "registered", "tva_status": sys.Status, "flag": made.Flag}}, now)
	})
	switch word, refused := registerRefusal(err); {
	case err == nil && made.Flag == callers.FlagNoOwner:
		back(w, r, "/systems", "registeredNoOwner", "warn")
	case err == nil:
		back(w, r, "/systems", "registered", "good")
	case refused:
		back(w, r, "/systems", word, "bad")
	default:
		a.fail(w, r, err)
	}
}
