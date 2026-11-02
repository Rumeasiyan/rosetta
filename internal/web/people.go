package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/jackc/pgx/v5"
)

func (a *App) mountPeople(mux *http.ServeMux) {
	mux.Handle("GET /people", a.signedIn(a.only(isAdmin, a.peoplePage)))
	mux.Handle("POST /people/grant", a.signedIn(a.only(isAdmin, a.post(a.grant))))
	mux.Handle("POST /people/{id}/end", a.signedIn(a.only(isAdmin, a.post(a.endGrant))))
}

// peopleData is the people screen's data.
type peopleData struct {
	Grants []people.Grant
	Roles  []string
}

// grantableRoles is what the form offers (R-ADM-06).
var grantableRoles = []string{people.Administrator, people.GlossaryKeeper, people.MemoryReviewer}

func (a *App) peopleData(ctx context.Context) (peopleData, error) {
	gs, err := people.AllLive(ctx, a.deps.Pool)
	if err != nil {
		return peopleData{}, err
	}
	return peopleData{Grants: gs, Roles: grantableRoles}, nil
}

func (a *App) peoplePage(w http.ResponseWriter, r *http.Request) {
	d, err := a.peopleData(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "people", "people")
	p.Data = d
	a.Render(w, r, http.StatusOK, "people", p)
}

// roleRefusal is the message for each refusal. What the person typed is never repeated.
func roleRefusal(err error) (string, bool) {
	for refusal, word := range map[error]string{
		people.ErrBadRole:           "badRole",
		people.ErrBadLanguage:       "badLanguage",
		people.ErrBadDate:           "badDate",
		people.ErrNoSuchPerson:      "noSuchPerson",
		people.ErrAlreadyHeld:       "alreadyHeld",
		people.ErrNoSuchGrant:       "noSuchGrant",
		people.ErrLastAdministrator: "lastAdmin",
	} {
		if errors.Is(err, refusal) {
			return word, true
		}
	}
	return "", false
}

// grant gives a role, with its audit entry in the same transaction.
func (a *App) grant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	f := r.PostForm
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		today, err := people.Today(ctx, tx)
		if err != nil {
			return err
		}
		req, err := people.ParseRequest(f.Get("role"), strings.TrimSpace(f.Get("language")), strings.TrimSpace(f.Get("ends_on")), today)
		if err != nil {
			return err
		}
		to, err := people.GrantRole(ctx, tx, f.Get("email"), c.sess.personID, req)
		if err != nil {
			return err
		}
		details := map[string]any{"change": "granted", "role": req.Role}
		if req.Language != nil {
			details["language"] = *req.Language
		}
		if req.EndsOn != nil {
			details["ends_on"] = req.EndsOn.Format(time.DateOnly)
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionRoleChange, ObjectType: "person",
			ObjectRef: to.Subject, Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: details}, now)
	})
	a.afterRoleChange(w, r, err, "granted")
}

// endGrant ends a role at once (R-ADM-06).
func (a *App) endGrant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	grantID, perr := strconv.ParseInt(r.PathValue("id"), 10, 64)
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		if perr != nil {
			return people.ErrNoSuchGrant
		}
		g, err := people.EndRole(ctx, tx, grantID, c.sess.personID)
		if err != nil {
			return err
		}
		details := map[string]any{"change": "ended", "role": g.Role}
		if g.Language != "" {
			details["language"] = string(g.Language)
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionRoleChange, ObjectType: "person",
			ObjectRef: g.Subject, Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: details}, now)
	})
	a.afterRoleChange(w, r, err, "ended")
}

func (a *App) afterRoleChange(w http.ResponseWriter, r *http.Request, err error, done string) {
	switch word, refused := roleRefusal(err); {
	case err == nil:
		back(w, r, "/people", done, "good")
	case refused:
		back(w, r, "/people", word, "bad")
	default:
		a.fail(w, r, err)
	}
}
