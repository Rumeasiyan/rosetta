package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/memory"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/jackc/pgx/v5"
)

// mountMemory adds the memory's screens. The administrator finds and removes (R-MEM-05); a reviewer
// also confirms and corrects (R-MEM-04), and acts only on their own language (s2).
func (a *App) mountMemory(mux *http.ServeMux) {
	mux.Handle("GET /memory", a.signedIn(a.only(mayReview, a.memoryScreen)))
	mux.Handle("GET /memory/{id}", a.signedIn(a.only(mayReview, a.memoryEntry)))
	mux.Handle("POST /memory/{id}/confirm", a.signedIn(a.only(isReviewer, a.post(a.memoryConfirm))))
	mux.Handle("POST /memory/{id}/correct", a.signedIn(a.only(isReviewer, a.post(a.memoryCorrect))))
	mux.Handle("POST /memory/{id}/remove", a.signedIn(a.only(mayReview, a.post(a.memoryRemove))))
}

func isReviewer(c *current) bool { return c.has(people.MemoryReviewer) }

func mayReview(c *current) bool { return isReviewer(c) || isAdmin(c) }

// reviewLanguages is the languages this person sees: nil, every one, for an administrator.
func reviewLanguages(c *current) []string {
	if isAdmin(c) {
		return []string{}
	}
	return languageNames(c.languagesOf(people.MemoryReviewer))
}

// reviews says this person is the reviewer for this entry's language: the language it is served in.
func reviews(c *current, e memory.Entry) bool {
	l, ok := i18n.Parse(e.To)
	return ok && slices.Contains(c.languagesOf(people.MemoryReviewer), l)
}

// sees says the entry is in this person's languages.
func sees(c *current, e memory.Entry) bool { return isAdmin(c) || reviews(c, e) }

// memoryScreenData is the memory screen: what waits, and what was found.
type memoryScreenData struct {
	Find     string
	Found    []memory.Entry
	Searched bool
	Rechecks []memory.Entry
	Waiting  int
}

func (a *App) memoryScreen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	langs := reviewLanguages(fromCtx(r))
	find := r.URL.Query().Get("find")
	found, err := memory.Find(ctx, a.deps.Pool, find, langs)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	waiting, total, err := memory.Rechecks(ctx, a.deps.Pool, langs)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "memory", "memory")
	p.Data = memoryScreenData{Find: find, Found: found, Searched: find != "", Rechecks: waiting, Waiting: total}
	a.Render(w, r, http.StatusOK, "memory", p)
}

// memoryEntryData is an entry's page: its two sides, and what this person may do.
type memoryEntryData struct {
	Entry     memory.Entry
	Left      int
	MayReview bool
}

// memoryOf finds the entry in the address, refusing one outside this person's languages as a denial.
func (a *App) memoryOf(w http.ResponseWriter, r *http.Request) (memory.Entry, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return memory.Entry{}, false
	}
	e, err := memory.Get(r.Context(), a.deps.Pool, id)
	if errors.Is(err, memory.ErrNoSuchEntry) {
		a.notFound(w, r)
		return e, false
	}
	if err != nil {
		a.fail(w, r, err)
		return e, false
	}
	if !sees(fromCtx(r), e) {
		a.deny(w, r, "Not this reviewer's language", "notYourMemoryLanguage")
		return e, false
	}
	return e, true
}

func (a *App) memoryEntry(w http.ResponseWriter, r *http.Request) {
	e, ok := a.memoryOf(w, r)
	if !ok {
		return
	}
	c := fromCtx(r)
	_, left, err := memory.Rechecks(r.Context(), a.deps.Pool, reviewLanguages(c))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "memory", "memory")
	p.Data = memoryEntryData{Entry: e, Left: left, MayReview: reviews(c, e) && e.State == memory.StateRecheck}
	a.Render(w, r, http.StatusOK, "memory-entry", p)
}

// memoryEvent is the audit entry for a reviewer's action: the number and the language, no words.
func memoryEvent(r *http.Request, action string, e memory.Entry) audit.Event {
	return audit.Event{Actor: fromCtx(r).actor(), Action: action, ObjectType: "memory entry", ObjectRef: strconv.FormatInt(e.ID, 10), Result: audit.ResultDone,
		Visitor: audit.Visitor(r), Details: map[string]any{"from": e.From, "to": e.To}}
}

// reviewAct does a confirm or a correction: only the reviewer for the entry's language may (R-MEM-04).
func (a *App) reviewAct(w http.ResponseWriter, r *http.Request, action, done string, work func(tx pgx.Tx, e memory.Entry, by int64) error) {
	e, ok := a.memoryOf(w, r)
	if !ok {
		return
	}
	c := fromCtx(r)
	if !reviews(c, e) {
		a.deny(w, r, "Not this reviewer's language", "notYourMemoryLanguage")
		return
	}
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		if err := work(tx, e, c.sess.personID); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, memoryEvent(r, action, e), now)
	})
	a.afterMemory(w, r, e, err, done)
}

func (a *App) memoryConfirm(w http.ResponseWriter, r *http.Request) {
	a.reviewAct(w, r, audit.ActionApproval, "memoryConfirmed", func(tx pgx.Tx, e memory.Entry, by int64) error {
		return memory.Confirm(r.Context(), tx, e.ID, by)
	})
}

func (a *App) memoryCorrect(w http.ResponseWriter, r *http.Request) {
	v := a.settings.Get(r.Context())
	a.reviewAct(w, r, audit.ActionChange, "memoryCorrected", func(tx pgx.Tx, e memory.Entry, by int64) error {
		return memory.Correct(r.Context(), tx, e.ID, r.PostForm.Get("text"), by, v)
	})
}

// memoryRemove takes a wrong entry out, recorded (R-MEM-05). The administrator may, and a reviewer for
// its language.
func (a *App) memoryRemove(w http.ResponseWriter, r *http.Request) {
	e, ok := a.memoryOf(w, r)
	if !ok {
		return
	}
	c := fromCtx(r)
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		if err := memory.Remove(r.Context(), tx, e.ID, c.sess.personID); err != nil {
			return err
		}
		ev := memoryEvent(r, audit.ActionDeletion, e)
		ev.Reason = "Removed from the memory"
		return audit.Record(r.Context(), tx, ev, now)
	})
	if err == nil {
		back(w, r, "/memory", "memoryRemoved", "good")
		return
	}
	a.afterMemory(w, r, e, err, "")
}

func (a *App) afterMemory(w http.ResponseWriter, r *http.Request, e memory.Entry, err error, done string) {
	path := "/memory/" + strconv.FormatInt(e.ID, 10)
	switch {
	case err == nil:
		back(w, r, path, done, "good")
	case errors.Is(err, memory.ErrNoSuchEntry):
		a.notFound(w, r)
	case errors.Is(err, memory.ErrNotRecheck):
		back(w, r, path, "memoryNotRecheck", "bad")
	case errors.Is(err, memory.ErrGone):
		back(w, r, path, "memoryGone", "bad")
	case errors.Is(err, memory.ErrBadText):
		back(w, r, path, "memoryBadText", "bad")
	default:
		a.fail(w, r, err)
	}
}
