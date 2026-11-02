package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/jackc/pgx/v5"
)

// mountGlossary adds the glossary's screens. Reading it needs a role of any kind (R-ADM-01),
// and a keeper acts only on their own language (s2).
func (a *App) mountGlossary(mux *http.ServeMux) {
	mux.Handle("GET /glossary", a.signedIn(a.only(hasAnyRole, a.glossaryList)))
	mux.Handle("GET /glossary/waiting", a.signedIn(a.only(mayKeep, a.glossaryWaiting)))
	mux.Handle("GET /glossary/propose", a.signedIn(a.only(mayKeep, a.glossaryProposeForm)))
	mux.Handle("POST /glossary/propose", a.signedIn(a.only(mayKeep, a.post(a.glossaryPropose))))
	mux.Handle("GET /glossary-versions", a.signedIn(a.only(hasAnyRole, a.glossaryVersions)))
	mux.Handle("GET /glossary-versions/{number}", a.signedIn(a.only(hasAnyRole, a.glossaryVersion)))
	mux.Handle("GET /glossary/{id}", a.signedIn(a.only(hasAnyRole, a.glossaryEntry)))
	mux.Handle("GET /glossary/{id}/history", a.signedIn(a.only(hasAnyRole, a.glossaryHistory)))
	mux.Handle("POST /glossary/{id}/check", a.signedIn(a.only(isKeeper, a.post(a.glossaryCheck))))
	mux.Handle("POST /glossary/{id}/refuse", a.signedIn(a.only(isKeeper, a.post(a.glossaryRefuse))))
}

// hasAnyRole is whoever holds any role: a grant, or a system of their own.
func hasAnyRole(c *current) bool { return len(c.grants) > 0 || len(c.owned) > 0 }

func isKeeper(c *current) bool { return c.has(people.GlossaryKeeper) }

// mayKeep opens the glossary's waiting work: keepers, and the administrator who names them.
func mayKeep(c *current) bool { return isKeeper(c) || isAdmin(c) }

// languagesOf is the languages this person holds a role in.
func (c *current) languagesOf(role string) []i18n.Lang {
	var out []i18n.Lang
	for _, g := range c.grants {
		if g.Role == role && g.Language != "" && !slices.Contains(out, g.Language) {
			out = append(out, g.Language)
		}
	}
	return out
}

// glossaryRefusal is the message for each refusal. What the person typed is never repeated.
func glossaryRefusal(err error) (string, bool) {
	for _, r := range []struct {
		err  error
		word string
	}{
		{glossary.ErrBadType, "glossaryBadType"},
		{glossary.ErrBadText, "glossaryBadText"},
		{glossary.ErrBadNote, "glossaryBadNote"},
		{glossary.ErrNoTerms, "glossaryNoTerms"},
		{glossary.ErrKeepDiffers, "glossaryKeepDiffers"},
		{glossary.ErrDuplicate, "glossaryDuplicate"},
		{glossary.ErrNotWaiting, "glossaryNotWaiting"},
		{glossary.ErrNotCurrent, "glossaryNotCurrent"},
		{glossary.ErrCorrectionWaits, "glossaryCorrectionWaits"},
		{glossary.ErrNothingChanged, "glossaryNothingChanged"},
		{glossary.ErrNoText, "glossaryNoText"},
		{glossary.ErrAlreadyChecked, "glossaryAlreadyChecked"},
		{glossary.ErrNoOutcomeNote, "glossaryNoOutcomeNote"},
	} {
		if errors.Is(err, r.err) {
			return r.word, true
		}
	}
	return "", false
}

func typeWord(kind string) string {
	switch kind {
	case glossary.TypePost:
		return "typePost"
	case glossary.TypeInstitution:
		return "typeInstitution"
	case glossary.TypePlace:
		return "typePlace"
	case glossary.TypeTerm:
		return "typeTerm"
	case glossary.TypeKeep:
		return "typeKeep"
	}
	return kind
}

func statusWord(status string) string {
	switch status {
	case glossary.StatusProposed:
		return "statusProposed"
	case glossary.StatusHeld:
		return "statusHeld"
	case glossary.StatusPublished:
		return "statusPublished"
	case glossary.StatusReplaced:
		return "statusReplaced"
	case glossary.StatusRefused:
		return "statusRefused"
	}
	return status
}

// glossaryListData is the list's data.
type glossaryListData struct {
	Entries []glossary.Entry
	Find    string
	Version int
	Full    bool // the page holds as many as it can: finding narrows it
	MayKeep bool
}

func (a *App) glossaryList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	find := r.URL.Query().Get("find")
	list, err := glossary.Published(ctx, a.deps.Pool, find)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	version, err := glossary.Current(ctx, a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "glossary", "glossary")
	p.Data = glossaryListData{Entries: list, Find: find, Version: version, Full: len(list) == glossary.PageLimit, MayKeep: mayKeep(fromCtx(r))}
	a.Render(w, r, http.StatusOK, "glossary", p)
}

// glossaryWaitingData is the keepers' list.
type glossaryWaitingData struct {
	Entries []glossary.Entry
	Yours   []i18n.Lang
}

func (a *App) glossaryWaiting(w http.ResponseWriter, r *http.Request) {
	yours := fromCtx(r).languagesOf(people.GlossaryKeeper)
	list, err := glossary.Waiting(r.Context(), a.deps.Pool, yours)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "glossaryWaiting", "glossary")
	p.Data = glossaryWaitingData{Entries: list, Yours: yours}
	a.Render(w, r, http.StatusOK, "glossary-waiting", p)
}

// glossaryFormData is the form for a term, new or corrected.
type glossaryFormData struct {
	Types []string
	Entry *glossary.Entry // the entry a correction starts from
}

func (a *App) glossaryProposeForm(w http.ResponseWriter, r *http.Request) {
	d := glossaryFormData{Types: glossary.Types}
	if raw := r.URL.Query().Get("corrects"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			a.notFound(w, r)
			return
		}
		e, err := glossary.Get(r.Context(), a.deps.Pool, id)
		if errors.Is(err, glossary.ErrNoSuchEntry) {
			a.notFound(w, r)
			return
		}
		if err != nil {
			a.fail(w, r, err)
			return
		}
		d.Entry = &e
	}
	p := a.page(r, "glossaryPropose", "glossary")
	p.Data = d
	a.Render(w, r, http.StatusOK, "glossary-propose", p)
}

func languageNames(langs []i18n.Lang) []string {
	out := make([]string, len(langs))
	for i, l := range langs {
		out[i] = string(l)
	}
	return out
}

// glossaryPropose adds the entry, with its audit entry and never its words.
func (a *App) glossaryPropose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	f := r.PostForm
	proposal := glossary.Proposal{Type: f.Get("type"), Note: f.Get("note"), By: c.sess.personID, Texts: map[i18n.Lang]string{}}
	for _, l := range i18n.Langs {
		proposal.Texts[l] = f.Get("text_" + string(l))
	}
	if raw := f.Get("replaces"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			a.notFound(w, r)
			return
		}
		proposal.Replaces = &id
	}
	var id int64
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		var concerned []i18n.Lang
		var err error
		if id, concerned, err = glossary.Propose(ctx, tx, proposal); err != nil {
			return err
		}
		details := map[string]any{"languages": languageNames(concerned), "type": proposal.Type}
		if proposal.Replaces != nil {
			details["replaces"] = strconv.FormatInt(*proposal.Replaces, 10)
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionCreation, ObjectType: "glossary entry",
			ObjectRef: strconv.FormatInt(id, 10), Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: details}, now)
	})
	switch word, refused := glossaryRefusal(err); {
	case err == nil:
		back(w, r, "/glossary/"+strconv.FormatInt(id, 10), "glossaryProposed", "good")
	case errors.Is(err, glossary.ErrNoSuchEntry):
		a.notFound(w, r)
	case refused:
		back(w, r, "/glossary/propose", word, "bad")
	default:
		a.fail(w, r, err)
	}
}

// glossaryVersionsData is the list of versions.
type glossaryVersionsData struct {
	Versions []glossary.Version
}

func (a *App) glossaryVersions(w http.ResponseWriter, r *http.Request) {
	list, err := glossary.Versions(r.Context(), a.deps.Pool)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "glossaryVersions", "glossary")
	p.Data = glossaryVersionsData{Versions: list}
	a.Render(w, r, http.StatusOK, "glossary-versions", p)
}

// glossaryVersionData is the glossary as it stood at one version (R-GLO-05).
type glossaryVersionData struct {
	Version glossary.Version
	Entries []glossary.Entry
	Find    string
	Full    bool
}

func (a *App) glossaryVersion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || n < 0 {
		a.notFound(w, r)
		return
	}
	v, err := glossary.VersionAt(ctx, a.deps.Pool, n)
	if errors.Is(err, glossary.ErrNoVersion) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	find := r.URL.Query().Get("find")
	list, err := glossary.AtVersion(ctx, a.deps.Pool, n, find)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "glossaryVersions", "glossary")
	p.Data = glossaryVersionData{Version: v, Entries: list, Find: find, Full: len(list) == glossary.PageLimit}
	a.Render(w, r, http.StatusOK, "glossary-version", p)
}

// glossaryEntryData is a term's page: its three bands, and what this person may do.
type glossaryEntryData struct {
	Entry      glossary.Entry
	Yours      []glossary.Term // the languages this keeper may check here
	MayCorrect bool
}

func entryID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

// mayAct is what a keeper can check of this entry: their own languages, while it waits.
func mayAct(c *current, e glossary.Entry) []glossary.Term {
	if !e.Waiting() {
		return nil
	}
	var out []glossary.Term
	for _, l := range c.languagesOf(people.GlossaryKeeper) {
		out = append(out, e.Term(l))
	}
	return out
}

func (a *App) glossaryEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := entryID(r)
	if !ok {
		a.notFound(w, r)
		return
	}
	e, err := glossary.Get(r.Context(), a.deps.Pool, id)
	if errors.Is(err, glossary.ErrNoSuchEntry) {
		a.notFound(w, r)
		return
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	c := fromCtx(r)
	p := a.page(r, "glossary", "glossary")
	p.Data = glossaryEntryData{Entry: e, Yours: mayAct(c, e), MayCorrect: e.Status == glossary.StatusPublished && mayKeep(c)}
	a.Render(w, r, http.StatusOK, "glossary-entry", p)
}

// glossaryHistoryData is an entry's chain of replacements (R-GLO-02).
type glossaryHistoryData struct {
	ID      int64
	Entries []glossary.Entry
}

func (a *App) glossaryHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := entryID(r)
	if !ok {
		a.notFound(w, r)
		return
	}
	list, err := glossary.Chain(r.Context(), a.deps.Pool, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if len(list) == 0 {
		a.notFound(w, r)
		return
	}
	p := a.page(r, "history", "glossary")
	p.Data = glossaryHistoryData{ID: id, Entries: list}
	a.Render(w, r, http.StatusOK, "glossary-history", p)
}

// keeperLanguage reads the language a keeper acts in, refusing any but their own: a denial,
// which PENSIEVE's list requires.
func (a *App) keeperLanguage(w http.ResponseWriter, r *http.Request) (i18n.Lang, bool) {
	l, ok := i18n.Parse(r.PostForm.Get("language"))
	if !ok || !slices.Contains(fromCtx(r).languagesOf(people.GlossaryKeeper), l) {
		a.deny(w, r, "Not this keeper's language", "notYourLanguage")
		return "", false
	}
	return l, true
}

// glossaryCheck is one keeper's check. The third publishes the entry, in this transaction with
// its audit entries.
func (a *App) glossaryCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	id, ok := entryID(r)
	if !ok {
		a.notFound(w, r)
		return
	}
	l, ok := a.keeperLanguage(w, r)
	if !ok {
		return
	}
	var done glossary.Checked
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		var err error
		if done, err = glossary.Check(ctx, tx, id, l, r.PostForm.Get("text"), c.sess.personID); err != nil {
			return err
		}
		ref := strconv.FormatInt(id, 10)
		if err := audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionApproval, ObjectType: "glossary entry", ObjectRef: ref,
			Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: map[string]any{"language": string(l), "corrected": done.Corrected}}, now); err != nil {
			return err
		}
		if !done.Published {
			return nil
		}
		details := map[string]any{"entry": ref}
		if done.Replaced != nil {
			details["replaced"] = strconv.FormatInt(*done.Replaced, 10)
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionChange, ObjectType: "glossary version",
			ObjectRef: strconv.Itoa(done.Version), Result: audit.ResultDone, Visitor: audit.Visitor(r), Details: details}, now)
	})
	word := "glossaryChecked"
	if done.Published {
		word = "glossaryPublished"
	}
	a.afterKeeper(w, r, id, err, word)
}

// glossaryRefuse ends a waiting entry with why, kept for the proposer and never sent to the audit.
func (a *App) glossaryRefuse(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c := fromCtx(r)
	id, ok := entryID(r)
	if !ok {
		a.notFound(w, r)
		return
	}
	l, ok := a.keeperLanguage(w, r)
	if !ok {
		return
	}
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		if err := glossary.Refuse(ctx, tx, id, l, r.PostForm.Get("note")); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Actor: c.actor(), Action: audit.ActionRejection, ObjectType: "glossary entry", ObjectRef: strconv.FormatInt(id, 10),
			Result: audit.ResultDone, Reason: "Refused by a keeper", Visitor: audit.Visitor(r), Details: map[string]any{"language": string(l)}}, now)
	})
	a.afterKeeper(w, r, id, err, "glossaryRefused")
}

func (a *App) afterKeeper(w http.ResponseWriter, r *http.Request, id int64, err error, done string) {
	path := "/glossary/" + strconv.FormatInt(id, 10)
	switch word, refused := glossaryRefusal(err); {
	case err == nil:
		back(w, r, path, done, "good")
	case errors.Is(err, glossary.ErrNoSuchEntry):
		a.notFound(w, r)
	case refused:
		back(w, r, path, word, "bad")
	default:
		a.fail(w, r, err)
	}
}
