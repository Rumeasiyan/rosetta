package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/tva"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SignIn is MJOLNIR's sign-in as the screens use it.
type SignIn interface {
	AuthURL(ctx context.Context, p oidc.Pending) (string, error)
	Exchange(ctx context.Context, code string, p oidc.Pending) (oidc.Tokens, error)
	Renew(ctx context.Context, refresh, nonce, subject string) (oidc.Tokens, error)
	Revoke(ctx context.Context, refresh string) error
}

// Deps is what the screens need.
type Deps struct {
	Pool      *pgxpool.Pool
	SignIn    SignIn
	PublicURL string // ROSETTA_PUBLIC_URL
	// SignOutURL is ROSETTA_SIGNOUT_URL: MJOLNIR's own sign-out page. It is required, and
	// the only place a sign-out sends the browser.
	SignOutURL string
	// SignInIssuer is ROSETTA_SIGNIN_ISSUER. A sign-out passes through both of MJOLNIR's
	// hosts, so pages holding the sign-out form name its origin in form-action too.
	SignInIssuer string
	// FirstAdmin is ROSETTA_FIRST_ADMIN: the MJOLNIR account that becomes the first
	// administrator, only while no administrator exists.
	FirstAdmin string
	// Outbox sends audit entries after each change. Nil sends none: they wait in
	// the outbox.
	Outbox Kicker
	// TVA reads the system register's published file when a system is registered
	// (R-CAL-01). Nil refuses every registration, as a file that cannot be read does.
	TVA tva.Reader
	// Sources is BUMBLEBEE's source list. Nil shows it as never read.
	Sources SourceList
	Logger  *slog.Logger
}

// Kicker starts sending what the outbox holds, without waiting for it.
type Kicker interface{ Kick() }

// signinCallTimeout is the longest ROSETTA waits on MJOLNIR for one person's request.
const signinCallTimeout = 10 * time.Second

// App is the screens.
type App struct {
	*Renderer
	deps     Deps
	sessions *sessions
	secure   bool
	settings *settings.Cached[settings.Values]
}

// New makes the screens.
func New(deps Deps) (*App, error) {
	if deps.SignOutURL == "" {
		return nil, errors.New("web: no address for MJOLNIR's sign-out page")
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}
	r, err := NewRenderer(deps.Logger)
	if err != nil {
		return nil, err
	}
	r.signOutOrigins = originsOf(deps.SignOutURL, deps.SignInIssuer)
	a := &App{Renderer: r, deps: deps, sessions: newSessions(), secure: strings.HasPrefix(deps.PublicURL, "https://")}
	a.settings = settings.NewCached(settings.Defaults(), func(ctx context.Context) (settings.Values, error) {
		return settings.Load(ctx, deps.Pool)
	}, settings.RefreshEvery, deps.Logger)
	return a, nil
}

// Mount adds sign-in and every screen to mux.
func (a *App) Mount(mux *http.ServeMux) {
	mux.Handle("GET "+StaticPrefix, staticHandler())
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, StaticPrefix+"favicon.ico", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /signin", a.gatePage)
	mux.HandleFunc("GET /signin/start", a.signinStart)
	mux.HandleFunc("GET "+oidc.CallbackPath, a.signinCallback)
	mux.HandleFunc("POST /signout", a.signout)
	mux.HandleFunc("GET /lang/{lang}", a.setLanguage)
	mux.Handle("GET /{$}", a.signedIn(a.home))
	a.mountPeople(mux)
	a.mountSettings(mux)
	a.mountSystems(mux)
	a.mountSources(mux)
	a.mountGate(mux)
	mux.HandleFunc("/", a.notFound)
}

// current is the signed-in person on this request, with their roles read again on
// every request, so a role that ends takes effect at once.
type current struct {
	token  string
	sess   session
	grants []people.Grant
	owned  []people.OwnedSystem
}

func (c *current) has(role string) bool {
	for _, g := range c.grants {
		if g.Role == role {
			return true
		}
	}
	return false
}

func isAdmin(c *current) bool { return c.has(people.Administrator) }

// actor is the signed-in person as an audit entry names them. Their post is not
// known until E-CAL-05.
func (c *current) actor() audit.Actor {
	return audit.Actor{PersonID: c.sess.personID, Subject: c.sess.subject, Name: c.sess.name, Post: audit.PostNotKnown}
}

// viewer is who the frame shows.
func (c *current) viewer() *Viewer {
	v := &Viewer{Name: c.sess.name}
	for _, g := range c.grants {
		w := RoleWord(g.Role)
		if !slices.Contains(v.Roles, w) {
			v.Roles = append(v.Roles, w)
		}
	}
	if len(c.owned) > 0 {
		v.Roles = append(v.Roles, "roleSystemOwner")
	}
	return v
}

// nav is the top bar: only the screens this person's roles open.
func nav(c *current) []NavItem {
	var items []NavItem
	if isAdmin(c) {
		items = append(items, NavItem{ID: "people", Word: "people", Href: "/people"}, NavItem{ID: "settings", Word: "settings", Href: "/settings"})
	}
	if seesSystems(c) {
		items = append(items, NavItem{ID: "systems", Word: "systems", Href: "/systems"}, NavItem{ID: "sources", Word: "sources", Href: "/sources"})
	}
	if isAdmin(c) {
		items = append(items, NavItem{ID: "gate", Word: "gate", Href: "/gate"})
	}
	return items
}

type currentKey struct{}

func fromCtx(r *http.Request) *current { return r.Context().Value(currentKey{}).(*current) }

// isBackground says the browser cannot simply be redirected: a form or an htmx
// request (CERBERUS's JOINING.md answers these the same way).
func isBackground(r *http.Request) bool {
	return r.Method != http.MethodGet || r.Header.Get("HX-Request") != ""
}

// signedIn lets a request through only with a live session, renewing it first when
// the person's own request needs it, never on a timer.
func (a *App) signedIn(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ck, err := r.Cookie(SessionCookie)
		if err != nil {
			a.toSignIn(w, r, "/signin")
			return
		}
		timer := r.Header.Get("X-Timer-Request") != ""
		sess, ok := a.sessions.get(ck.Value, timer)
		if !ok {
			a.setCookie(w, SessionCookie, "", -1)
			a.toSignIn(w, r, "/signin/start")
			return
		}
		if !a.sessions.now().Before(sess.expires) {
			if timer {
				// Only a person's own request renews; a tab left open must end.
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			switch err := a.renew(r.Context(), ck.Value); {
			case errors.Is(err, oidc.ErrRefused):
				a.setCookie(w, SessionCookie, "", -1)
				a.toSignIn(w, r, "/signin/start")
				return
			case err != nil:
				a.deps.Logger.Warn("sign-in renewal", "error", err)
				a.Render(w, r, http.StatusServiceUnavailable, "message", Page{Lang: Language(r), Path: r.URL.Path, Title: "rosetta", Flash: "signInUnavailable", FlashTone: "bad"})
				return
			}
			sess, _ = a.sessions.get(ck.Value, timer)
		}
		c := &current{token: ck.Value, sess: sess}
		if c.grants, err = people.Live(r.Context(), a.deps.Pool, sess.personID); err != nil {
			a.fail(w, r, err)
			return
		}
		if c.owned, err = people.Owned(r.Context(), a.deps.Pool, sess.subject); err != nil {
			a.fail(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), currentKey{}, c)))
	})
}

// renew sends the session's refresh token once. One request at a time holds the
// session's lock, and the others use what it brought. The call does not end with
// the person's request: if their browser went away, the new token must still be
// kept, since the old one cannot be sent twice.
func (a *App) renew(ctx context.Context, tok string) error {
	first, ok := a.sessions.get(tok, false)
	if !ok {
		return oidc.ErrRefused
	}
	first.renewal.Lock()
	defer first.renewal.Unlock()
	sess, ok := a.sessions.get(tok, false)
	if !ok {
		return oidc.ErrRefused
	}
	if a.sessions.now().Before(sess.expires) {
		return nil // another request has just renewed it
	}
	if sess.refresh == "" {
		a.sessions.end(tok)
		return oidc.ErrRefused
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), signinCallTimeout)
	defer cancel()
	tokens, err := a.deps.SignIn.Renew(cctx, sess.refresh, sess.nonce, sess.subject)
	if errors.Is(err, oidc.ErrRefused) {
		a.sessions.end(tok)
		return err
	}
	if err != nil {
		return err
	}
	a.sessions.renewed(tok, tokens)
	if err := people.Seen(cctx, a.deps.Pool, sess.personID); err != nil {
		a.deps.Logger.Error("person not marked seen", "error", err)
	}
	return nil
}

// toSignIn sends a person without a session on: a page straight there, and a form
// or an htmx request a 401 that makes htmx reload, so their work is not lost unseen.
func (a *App) toSignIn(w http.ResponseWriter, r *http.Request, where string) {
	if isBackground(r) {
		w.Header().Set("HX-Refresh", "true")
		a.gate(w, r, http.StatusUnauthorized, "expired", "warn", "/")
		return
	}
	http.Redirect(w, r, where+"?next="+url.QueryEscape(r.URL.Path), http.StatusSeeOther)
}

// page is the data every signed-in screen starts from. A flash message arrives in
// the query after a form's redirect; only known words show, so nothing a link
// carries reaches the page.
func (a *App) page(r *http.Request, title, here string) Page {
	c := fromCtx(r)
	p := Page{Lang: Language(r), Path: r.URL.Path, Title: title, Here: here, Nav: nav(c), Viewer: c.viewer(), CSRF: c.sess.csrf}
	if m := r.URL.Query().Get("m"); m != "" && i18n.Has(m) {
		p.Flash = m
		switch t := r.URL.Query().Get("tone"); t {
		case "good", "warn", "bad":
			p.FlashTone = t
		default:
			p.FlashTone = "good"
		}
	}
	return p
}

// back redirects to path with a message.
func back(w http.ResponseWriter, r *http.Request, path, msg, tone string) {
	http.Redirect(w, r, path+"?m="+url.QueryEscape(msg)+"&tone="+tone, http.StatusSeeOther)
}

// inTx runs work in one transaction, which also holds its audit entries, and once
// they are safely kept starts sending them (B 13: straight after each change).
func (a *App) inTx(r *http.Request, work func(tx pgx.Tx, now time.Time) error) error {
	err := pgx.BeginFunc(r.Context(), a.deps.Pool, func(tx pgx.Tx) error { return work(tx, time.Now()) })
	if err == nil && a.deps.Outbox != nil {
		a.deps.Outbox.Kick()
	}
	return err
}

// only lets the request through when allowed says so. Anything else is a denial,
// which PENSIEVE's list requires (P-ING-02). The entry names the route, never the
// address as sent.
func (a *App) only(allowed func(*current) bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := fromCtx(r)
		if allowed(c) {
			next(w, r)
			return
		}
		a.deny(w, r, "Role does not allow this", "notAllowed")
	}
}

// post checks the form's token, so another site cannot make the person's browser
// change anything: the session cookie alone is never enough.
func (a *App) post(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		if r.ParseForm() != nil || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(fromCtx(r).sess.csrf)) != 1 {
			a.deny(w, r, "Form token did not match", "badForm")
			return
		}
		next(w, r)
	}
}

// deny records one denial and shows the person why.
func (a *App) deny(w http.ResponseWriter, r *http.Request, reason, word string) {
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		return audit.Record(r.Context(), tx, audit.Event{Actor: fromCtx(r).actor(), Action: audit.ActionDenial, ObjectType: "screen",
			ObjectRef: r.Pattern, Result: audit.ResultDenied, Reason: reason, Visitor: audit.Visitor(r)}, now)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p := a.page(r, "rosetta", "")
	p.Flash, p.FlashTone = word, "bad"
	a.Render(w, r, http.StatusForbidden, "message", p)
}

func (a *App) fail(w http.ResponseWriter, r *http.Request, err error) {
	a.deps.Logger.Error("screen", "route", r.Pattern, "error", err)
	a.Render(w, r, http.StatusInternalServerError, "message", Page{Lang: Language(r), Path: r.URL.Path, Title: "rosetta", Flash: "somethingWrong", FlashTone: "bad"})
}

func (a *App) notFound(w http.ResponseWriter, r *http.Request) {
	a.Render(w, r, http.StatusNotFound, "message", Page{Lang: Language(r), Path: "/", Title: "rosetta", Flash: "notFound", FlashTone: "warn"})
}

// gate shows the sign-in gate with a message.
func (a *App) gate(w http.ResponseWriter, r *http.Request, status int, msg, tone, next string) {
	a.Render(w, r, status, "signin", Page{Lang: Language(r), Path: "/signin", Title: "signIn", Flash: msg, FlashTone: tone, Next: SafeNext(next)})
}

func (a *App) gatePage(w http.ResponseWriter, r *http.Request) {
	a.gate(w, r, http.StatusOK, "", "", r.URL.Query().Get("next"))
}

// homeData is what the home screen shows beyond the frame.
type homeData struct {
	Grants []people.Grant
	Owned  []people.OwnedSystem
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	c := fromCtx(r)
	p := a.page(r, "welcome", "")
	p.Data = homeData{Grants: c.grants, Owned: c.owned}
	a.Render(w, r, http.StatusOK, "home", p)
}

// setLanguage keeps the picked language and returns to the page it was picked on
// (R-NFR-05). Signed in, it is remembered on the person too.
func (a *App) setLanguage(w http.ResponseWriter, r *http.Request) {
	l, ok := i18n.Parse(r.PathValue("lang"))
	if !ok {
		a.notFound(w, r)
		return
	}
	a.setLanguageCookie(w, string(l))
	if ck, err := r.Cookie(SessionCookie); err == nil {
		if sess, ok := a.sessions.get(ck.Value, false); ok {
			if err := people.SetLanguage(r.Context(), a.deps.Pool, sess.personID, string(l)); err != nil {
				a.deps.Logger.Error("screen language not kept", "error", err)
			}
		}
	}
	http.Redirect(w, r, SafeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
}

// ---- sign-in ---------------------------------------------------------------

func (a *App) signinStart(w http.ResponseWriter, r *http.Request) {
	pd := oidc.NewPending()
	ctx, cancel := context.WithTimeout(r.Context(), signinCallTimeout)
	defer cancel()
	u, err := a.deps.SignIn.AuthURL(ctx, pd)
	if err != nil {
		a.deps.Logger.Error("sign-in", "step", "start", "error", err)
		a.gate(w, r, http.StatusBadGateway, "signInUnavailable", "bad", r.URL.Query().Get("next"))
		return
	}
	a.sessions.start(pd, SafeNext(r.URL.Query().Get("next")))
	a.setCookie(w, SigninCookie, pd.State, int(PendingLimit.Seconds()))
	http.Redirect(w, r, u, http.StatusSeeOther)
}

// signinCallback finishes a sign-in. What MJOLNIR or anyone else put in the query
// other than the code and state is never read, logged or shown.
func (a *App) signinCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	ck, err := r.Cookie(SigninCookie)
	a.setCookie(w, SigninCookie, "", -1)
	// The state must match this browser's own sign-in, used once.
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(ck.Value), []byte(state)) != 1 {
		a.gate(w, r, http.StatusBadRequest, "signInFailed", "bad", "/")
		return
	}
	pd, ok := a.sessions.take(state)
	if !ok || q.Get("code") == "" {
		a.gate(w, r, http.StatusBadRequest, "signInFailed", "bad", "/")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), signinCallTimeout)
	defer cancel()
	tokens, err := a.deps.SignIn.Exchange(ctx, q.Get("code"), pd.Pending)
	if err != nil {
		a.deps.Logger.Warn("sign-in", "step", "exchange", "error", err)
		a.gate(w, r, http.StatusUnauthorized, "signInFailed", "bad", pd.next)
		return
	}
	person, err := a.admit(r, tokens.Claims)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	// A browser signing in again leaves no older session or token behind.
	if old, err := r.Cookie(SessionCookie); err == nil {
		a.revoke(r.Context(), a.sessions.end(old.Value))
	}
	a.setLanguageCookie(w, person.Language)
	a.setCookie(w, SessionCookie, a.sessions.create(person.ID, person.Subject, person.Name, person.Email, pd.Nonce, tokens), 0)
	http.Redirect(w, r, pd.next, http.StatusSeeOther)
}

// admit keeps the person's record, settles their screen language (a pick made
// before signing in, else what they chose last time, else the browser's) and makes
// the first administrator if this is them.
func (a *App) admit(r *http.Request, c oidc.Claims) (people.Person, error) {
	ctx := r.Context()
	var person people.Person
	err := a.inTx(r, func(tx pgx.Tx, now time.Time) error {
		var err error
		if person, err = people.Upsert(ctx, tx, c.Subject, c.Name, c.Email); err != nil {
			return err
		}
		want := person.Language
		if ck, err := r.Cookie(LangCookie); err == nil {
			if l, ok := i18n.Parse(ck.Value); ok {
				want = string(l)
			}
		} else if person.IsNew {
			want = string(Language(r))
		}
		if want != person.Language {
			if err := people.SetLanguage(ctx, tx, person.ID, want); err != nil {
				return err
			}
			person.Language = want
		}
		first, err := people.FirstAdministrator(ctx, tx, person, a.deps.FirstAdmin)
		if err != nil || !first {
			return err
		}
		return audit.Record(ctx, tx, audit.Event{Actor: audit.System, Action: audit.ActionRoleChange, ObjectType: "person",
			ObjectRef: person.Subject, Result: audit.ResultDone, Reason: "First administrator, from the bootstrap setting",
			Visitor: audit.Visitor(r), Details: map[string]any{"change": "granted", "role": people.Administrator}}, now)
	})
	return person, err
}

func (a *App) setLanguageCookie(w http.ResponseWriter, language string) {
	a.setCookie(w, LangCookie, language, int((365 * 24 * time.Hour).Seconds()))
}

// revoke ends a refresh token. MJOLNIR being out of reach does not stop the person
// signing out of ROSETTA.
func (a *App) revoke(ctx context.Context, refresh string) {
	if refresh == "" {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), signinCallTimeout)
	defer cancel()
	if err := a.deps.SignIn.Revoke(cctx, refresh); err != nil {
		a.deps.Logger.Warn("sign-in revocation", "error", err)
	}
}

// signout does what MJOLNIR's docs/SESSION-INTERFACE.md gives a system's own button:
// revoke the token, end the session here, then send the browser to MJOLNIR's page, or
// the person stays signed in there and the next sign-in passes with no form. MJOLNIR's
// page does not come back. Nothing sent can change where the browser goes.
func (a *App) signout(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(SessionCookie)
	if err == nil {
		if sess, ok := a.sessions.get(ck.Value, false); ok {
			r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
			if r.ParseForm() != nil || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(sess.csrf)) != 1 {
				a.gate(w, r, http.StatusForbidden, "badForm", "bad", "/")
				return
			}
			a.revoke(r.Context(), a.sessions.end(ck.Value))
		}
	}
	a.setCookie(w, SessionCookie, "", -1)
	http.Redirect(w, r, a.deps.SignOutURL, http.StatusSeeOther)
}
