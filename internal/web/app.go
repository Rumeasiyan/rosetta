package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/Rumeasiyan/rosetta/internal/people"
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
	Logger    *slog.Logger
}

// signinCallTimeout is the longest ROSETTA waits on MJOLNIR for one person's request.
const signinCallTimeout = 10 * time.Second

// App is the screens.
type App struct {
	*Renderer
	deps     Deps
	sessions *sessions
	secure   bool
}

// New makes the screens.
func New(deps Deps) (*App, error) {
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}
	r, err := NewRenderer(deps.Logger)
	if err != nil {
		return nil, err
	}
	return &App{Renderer: r, deps: deps, sessions: newSessions(), secure: strings.HasPrefix(deps.PublicURL, "https://")}, nil
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
	mux.Handle("GET /{$}", a.signedIn(a.home))
	mux.HandleFunc("/", a.notFound)
}

// current is the signed-in person on this request.
type current struct {
	token string
	sess  session
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

// page is the data every signed-in screen starts from.
func (a *App) page(r *http.Request, title string) Page {
	c := fromCtx(r)
	return Page{Lang: Language(r), Path: r.URL.Path, Title: title, Viewer: &Viewer{Name: c.sess.name}, CSRF: c.sess.csrf}
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

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	a.Render(w, r, http.StatusOK, "home", a.page(r, "welcome"))
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

// admit keeps the person's record, and settles their screen language: a pick made
// before signing in, else what they chose last time, else the browser's.
func (a *App) admit(r *http.Request, c oidc.Claims) (people.Person, error) {
	ctx := r.Context()
	person, err := people.Upsert(ctx, a.deps.Pool, c.Subject, c.Name, c.Email)
	if err != nil {
		return person, err
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
		if err := people.SetLanguage(ctx, a.deps.Pool, person.ID, want); err != nil {
			return person, err
		}
		person.Language = want
	}
	return person, nil
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
	a.gate(w, r, http.StatusOK, "signedOut", "good", "/")
}
