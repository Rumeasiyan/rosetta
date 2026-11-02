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
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/jackc/pgx/v5"
)

// tvaReadTimeout is the longest a registration waits on TVA's file.
const tvaReadTimeout = 10 * time.Second

func (a *App) mountSystems(mux *http.ServeMux) {
	mux.Handle("GET /systems", a.signedIn(a.only(seesSystems, a.systemsPage)))
	mux.Handle("POST /systems/register", a.signedIn(a.only(isAdmin, a.post(a.registerSystem))))
	mux.Handle("GET /systems/{id}", a.signedIn(a.forSystem(a.systemPage)))
	mux.Handle("POST /systems/{id}/limits", a.signedIn(a.only(isAdmin, a.post(a.forSystem(a.setLimits)))))
	mux.Handle("POST /systems/{id}/keys", a.signedIn(a.post(a.forSystem(a.issueKey))))
	mux.Handle("POST /systems/{id}/keys/{key}/off", a.signedIn(a.post(a.forSystem(a.turnOffKey))))
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
	Keys   []callers.Key
	Admin  bool
	Limits []callers.LimitChange
}

func (a *App) systemPage(w http.ResponseWriter, r *http.Request, s callers.Caller) {
	keys, err := callers.Keys(r.Context(), a.deps.Pool, s.ID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	changes, err := callers.LimitHistory(r.Context(), a.deps.Pool, s.Reference)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "systems", "systems")
	p.Data = systemData{System: s, Keys: keys, Admin: isAdmin(fromCtx(r)), Limits: changes}
	a.Render(w, r, http.StatusOK, "system", p)
}

func systemPath(s callers.Caller) string { return "/systems/" + strconv.FormatInt(s.ID, 10) }

// setLimits changes a system's limits, with history and an audit entry in one
// transaction. The administrator's only: a system does not set its own (R-CAL-07).
func (a *App) setLimits(w http.ResponseWriter, r *http.Request, s callers.Caller) {
	ctx := r.Context()
	c := fromCtx(r)
	l, err := callers.ParseLimits(r.PostForm.Get("per_minute"), r.PostForm.Get("at_once"))
	if err != nil {
		back(w, r, systemPath(s), "limitsBad", "bad")
		return
	}
	changed := false
	err = a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		old, did, err := callers.SetLimits(ctx, tx, s, l, c.sess.personID)
		if err != nil || !did {
			return err
		}
		changed = true
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionSettingChange, ObjectType: "calling system",
			ObjectRef: s.Reference, Result: audit.ResultDone, Visitor: audit.Visitor(r),
			Details: map[string]any{"old_per_minute": strconv.Itoa(old.PerMinute), "old_at_once": strconv.Itoa(old.AtOnce),
				"per_minute": strconv.Itoa(l.PerMinute), "at_once": strconv.Itoa(l.AtOnce)}}, now)
	})
	switch {
	case err != nil:
		a.fail(w, r, err)
	case changed:
		back(w, r, systemPath(s), "limitsSaved", "good")
	default:
		back(w, r, systemPath(s), "settingUnchanged", "warn")
	}
}

// keyShown is the one screen that holds a key, once. It is not a redirect: the key
// goes in no address and is not kept (R-CAL-03; Render sends no-store).
type keyShown struct {
	System callers.Caller
	Key    string
	Prefix string
	Signs  bool
}

// keyEvent is the audit entry for a key: the system and the prefix are in object_ref,
// which PENSIEVE does not scan, as TVA's references are opaque and could look like
// anything. Never the key, its hash or its public key.
func keyEvent(c *current, r *http.Request, s callers.Caller, k callers.Key, change string) audit.Event {
	return audit.Event{Actor: c.actor(), Action: audit.ActionKeyChange, ObjectType: "caller key",
		ObjectRef: s.Reference + " " + k.Prefix, Result: audit.ResultDone, Visitor: audit.Visitor(r),
		Details: map[string]any{"change": change, "signed": k.Signs}}
}

// issueKey makes a key with its audit entry in one transaction, and shows it once.
func (a *App) issueKey(w http.ResponseWriter, r *http.Request, s callers.Caller) {
	ctx := r.Context()
	c := fromCtx(r)
	public, err := callers.ParsePublicKey(r.PostForm.Get("public_key"))
	if err != nil {
		back(w, r, systemPath(s), "keyBadPublic", "bad")
		return
	}
	var key string
	var made callers.Key
	err = a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		var err error
		if key, made, err = callers.Issue(ctx, tx, s.ID, c.sess.personID, public); err != nil {
			return err
		}
		return audit.Record(ctx, tx, keyEvent(c, r, s, made, "issued"), now)
	})
	switch {
	case errors.Is(err, callers.ErrTooManyKeys):
		back(w, r, systemPath(s), "keyTooMany", "bad")
	case err != nil:
		a.fail(w, r, err)
	default:
		p := a.page(r, "keyIssued", "systems")
		p.Data = keyShown{System: s, Key: key, Prefix: made.Prefix, Signs: made.Signs}
		a.Render(w, r, http.StatusOK, "key-issued", p)
	}
}

// turnOffKey ends a key at once. Only a key of this system: the address cannot reach
// another's.
func (a *App) turnOffKey(w http.ResponseWriter, r *http.Request, s callers.Caller) {
	ctx := r.Context()
	c := fromCtx(r)
	keyID, perr := strconv.ParseInt(r.PathValue("key"), 10, 64)
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		if perr != nil {
			return callers.ErrNoSuchKey
		}
		k, err := callers.TurnOff(ctx, tx, s.ID, keyID, c.sess.personID)
		if err != nil {
			return err
		}
		return audit.Record(ctx, tx, keyEvent(c, r, s, k, "turned off"), now)
	})
	switch {
	case errors.Is(err, callers.ErrNoSuchKey):
		back(w, r, systemPath(s), "keyNoSuch", "bad")
	case err != nil:
		a.fail(w, r, err)
	default:
		back(w, r, systemPath(s), "keyTurnedOff", "good")
	}
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
		starts := a.settings.Get(ctx)
		limits := callers.Limits{PerMinute: starts.Of(settings.CallerPerMinute), AtOnce: starts.Of(settings.CallerAtOnce)}
		if made, err = callers.Register(ctx, tx, sys, limits, c.sess.personID); err != nil {
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
