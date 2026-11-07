package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/oidc"
)

// Sessions live in memory, like PENSIEVE's: ROSETTA is one always-on process with
// no session table in Annex A, and a restart only asks people through a sign-in
// that passes with no form while MJOLNIR's session is live.
//
// How long a session lasts is MJOLNIR's to say, through a renewal it refuses
// (docs/SESSION-INTERFACE.md); ROSETTA holds no copy of its limits. ForgetAfter
// only keeps memory small.
const (
	ForgetAfter  = 12 * time.Hour
	PendingLimit = 10 * time.Minute

	SessionCookie = "rosetta_session"
	SigninCookie  = "rosetta_signin"

	// HoldCookie stops a sign-in starting by itself, so the sign-in page and its button show.
	// It says nothing about who anyone is, and is never cleared: it only runs out.
	HoldCookie = "rosetta_signin_hold"
	HoldLimit  = 5 * time.Minute
)

// session is one signed-in person. Its fields change under sessions.mu; renewing
// holds renewal, so one refresh token is only ever sent once.
type session struct {
	personID int64
	subject  string
	name     string
	email    string
	post     string // the post at sign-in, for audit entries (E-CAL-05)
	csrf     string
	seen     time.Time

	refresh string
	nonce   string
	expires time.Time

	renewal *sync.Mutex
}

type pending struct {
	oidc.Pending
	next    string
	created time.Time
}

// sessions holds the signed-in people and the sign-ins on their way.
type sessions struct {
	mu      sync.Mutex
	now     func() time.Time
	byToken map[string]*session
	waiting map[string]pending // by state
}

func newSessions() *sessions {
	return &sessions{now: time.Now, byToken: map[string]*session{}, waiting: map[string]pending{}}
}

// sweep drops what has run out. Called under mu on every change, so memory stays
// small without a timer (AGENTS C6).
func (s *sessions) sweep() {
	now := s.now()
	for k, v := range s.byToken {
		if now.Sub(v.seen) > ForgetAfter {
			delete(s.byToken, k)
		}
	}
	for k, v := range s.waiting {
		if now.Sub(v.created) > PendingLimit {
			delete(s.waiting, k)
		}
	}
}

func (s *sessions) start(p oidc.Pending, next string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.waiting[p.State] = pending{Pending: p, next: next, created: s.now()}
}

// take returns and forgets the sign-in with this state: each is used once.
func (s *sessions) take(state string) (pending, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	p, ok := s.waiting[state]
	delete(s.waiting, state)
	return p, ok
}

// create makes a session and returns its cookie's value.
func (s *sessions) create(personID int64, subject, name, email, post, nonce string, tokens oidc.Tokens) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	tok := oidc.Random()
	s.byToken[tok] = &session{personID: personID, subject: subject, name: name, email: email, post: post, csrf: oidc.Random(), seen: s.now(),
		refresh: tokens.Refresh, nonce: nonce, expires: tokens.Expires, renewal: &sync.Mutex{}}
	return tok
}

// get finds a live session and returns a copy. A request a page makes on a timer
// does not count as the person being seen (X-Timer-Request, CERBERUS's JOINING.md).
func (s *sessions) get(tok string, timer bool) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	v, ok := s.byToken[tok]
	if !ok {
		return session{}, false
	}
	if !timer {
		v.seen = s.now()
	}
	return *v, true
}

// renewed keeps what a renewal returned.
func (s *sessions) renewed(tok string, tokens oidc.Tokens) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.byToken[tok]; ok {
		v.refresh, v.expires = tokens.Refresh, tokens.Expires
	}
}

// end forgets a session and returns its refresh token, for revoking.
func (s *sessions) end(tok string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.byToken[tok]
	if !ok {
		return ""
	}
	delete(s.byToken, tok)
	return v.refresh
}

// setCookie writes a host-only cookie (no Domain), HttpOnly and SameSite=Lax;
// Secure unless ROSETTA runs on this machine over plain http (E-ADM-01).
func (a *App) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
}

// seal writes a sign-in into a cookie's value, signed with a key that lives only in this process, so
// nothing is kept here for someone who may never come back. A restart loses it, as it loses every
// sign-in in hand. The value is the browser's own, so it is signed and needs no secrecy.
func (a *App) seal(pd oidc.Pending) string {
	body := strings.Join([]string{pd.State, pd.Nonce, pd.Verifier, strconv.FormatInt(a.sessions.now().Add(PendingLimit).Unix(), 10)}, ".")
	return body + "." + a.sealSum(body)
}

func (a *App) sealSum(body string) string {
	mac := hmac.New(sha256.New, a.sealKey)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// unseal reads what seal wrote, while it lasts.
func (a *App) unseal(value string) (oidc.Pending, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 5 {
		return oidc.Pending{}, false
	}
	body := strings.Join(parts[:4], ".")
	if !hmac.Equal([]byte(parts[4]), []byte(a.sealSum(body))) {
		return oidc.Pending{}, false
	}
	until, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || a.sessions.now().Unix() >= until {
		return oidc.Pending{}, false
	}
	return oidc.Pending{State: parts[0], Nonce: parts[1], Verifier: parts[2], SignUp: true}, true
}
