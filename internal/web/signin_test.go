package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
)

func cookieNamed(res page, name string) *http.Cookie {
	h := http.Response{Header: res.header}
	for _, c := range h.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestSignInMakesThePersonAndASession(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	who := subject("person")
	if p := b.signIn(who); p.status != 200 || !strings.Contains(p.body, "Test "+who) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	var name, email string
	var first, last time.Time
	ctx := context.Background()
	err := w.pool.QueryRow(ctx, `SELECT name, email, first_seen_at, last_seen_at FROM person WHERE mjolnir_subject = $1`, who).Scan(&name, &email, &first, &last)
	if err != nil {
		t.Fatal(err)
	}
	if name != "Test "+who || email != who+"@example.invalid" {
		t.Fatalf("%q %q", name, email)
	}
	// A second sign-in moves last_seen_at and keeps one row: no sign-in history.
	w.exec(`UPDATE person SET name = 'Older', first_seen_at = first_seen_at - interval '1 day' WHERE mjolnir_subject = $1`, who)
	w.control("mjolnir", "form-always", nil)
	w.browser().signIn(who)
	var rows int
	var name2 string
	var last2 time.Time
	w.pool.QueryRow(ctx, `SELECT count(*), max(name), max(last_seen_at) FROM person WHERE mjolnir_subject = $1`, who).Scan(&rows, &name2, &last2)
	if rows != 1 || name2 != "Test "+who || !last2.After(last) {
		t.Fatalf("%d %q %v %v", rows, name2, last2, last)
	}
}

func TestCookiesAreHostOnlyHttpOnlyLax(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	req, _ := http.NewRequest(http.MethodGet, w.screens.URL+"/signin/start", nil)
	p := b.noFollow(req)
	c := cookieNamed(p, SigninCookie)
	if c == nil || c.Domain != "" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("%+v", c)
	}
	if c.Secure {
		t.Fatal("plain http on this machine cannot use Secure")
	}
	// Anything else is Secure.
	w.app.secure = strings.HasPrefix("https://rosetta.epc.test", "https://")
	p = b.noFollow(req)
	if c := cookieNamed(p, SigninCookie); c == nil || !c.Secure {
		t.Fatalf("%+v", c)
	}
	w.app.secure = false

	// The session cookie itself.
	fresh := w.browser()
	form := fresh.get("/signin/start")
	vals := url.Values{"own": {subject("cookie")}}
	for _, m := range formField.FindAllStringSubmatch(form.body, -1) {
		vals.Set(m[1], strings.ReplaceAll(m[2], "&amp;", "&"))
	}
	fresh.client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if strings.HasSuffix(via[len(via)-1].URL.Path, "/oauth2/auth") {
			return nil
		}
		return http.ErrUseLastResponse
	}
	res, err := fresh.client.PostForm(w.standin.URL+"/mjolnir/oauth2/auth", vals)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var session *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == SessionCookie {
			session = c
		}
	}
	if session == nil || session.Domain != "" || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
		t.Fatalf("%+v", session)
	}
}

func TestCallbackRefusals(t *testing.T) {
	w := newWorld(t)
	// With no sign-in on its way, whatever comes in the query is refused and not repeated.
	marker := "CALLERTEXTMARKER"
	p := w.browser().get(callbackURL(url.Values{"code": {"x"}, "state": {"y"}, "error_description": {marker}}))
	if p.status != 400 || strings.Contains(p.body, marker) {
		t.Fatalf("%d", p.status)
	}
	// The state must be this browser's.
	b := w.browser()
	req, _ := http.NewRequest(http.MethodGet, w.screens.URL+"/signin/start", nil)
	first := b.noFollow(req)
	loc, _ := url.Parse(first.header.Get("Location"))
	state := loc.Query().Get("state")
	if q := loc.Query().Get("prompt"); q != "" {
		t.Fatalf("prompt=%s", q)
	}
	if p := w.browser().get(callbackURL(url.Values{"code": {"x"}, "state": {state}})); p.status != 400 {
		t.Fatalf("another browser's state: %d", p.status)
	}
	// No code.
	if p := b.get(callbackURL(url.Values{"state": {state}, "error": {marker}})); p.status != 400 || strings.Contains(p.body, marker) {
		t.Fatalf("no code: %d", p.status)
	}
	// Used once: the state above was just spent.
	req, _ = http.NewRequest(http.MethodGet, w.screens.URL+callbackURL(url.Values{"code": {"x"}, "state": {state}}), nil)
	req.AddCookie(&http.Cookie{Name: SigninCookie, Value: state})
	if p := w.browser().do(req); p.status != 400 {
		t.Fatalf("a state used twice: %d", p.status)
	}
	// A code MJOLNIR does not know.
	b2 := w.browser()
	again := b2.noFollow(mustGet(w.screens.URL + "/signin/start"))
	loc, _ = url.Parse(again.header.Get("Location"))
	if p := b2.get(callbackURL(url.Values{"code": {"not-a-code"}, "state": {loc.Query().Get("state")}})); p.status != 401 {
		t.Fatalf("unknown code: %d", p.status)
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal("caller text reached a log")
	}
}

func callbackURL(q url.Values) string { return "/signin/callback?" + q.Encode() }

func mustGet(u string) *http.Request {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		panic(err)
	}
	return req
}

// B on a browser that still holds A's cookie is not served as A: CERBERUS names B, and A's
// session ends. The same name, or none, changes nothing, and the header never starts a session.
func TestAnotherPersonAtTheBrowserEndsTheSession(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	who := subject("person")
	b.signIn(who)
	as := func(account string) page {
		req := mustGet(w.screens.URL + "/")
		if account != "" {
			req.Header.Set("X-Cerberus-Account", account)
		}
		return b.noFollow(req)
	}
	for _, account := range []string{"", who} {
		if p := as(account); p.status != 200 || !strings.Contains(p.body, "Test "+who) {
			t.Fatalf("%q: %d", account, p.status)
		}
	}
	if p := as(subject("other")); p.status != 303 || strings.Contains(p.body, "Test "+who) {
		t.Fatalf("another person: %d %s", p.status, p.body)
	}
	if p := as(who); p.status != 303 {
		t.Fatalf("the session should be gone, and the header not start one: %d", p.status)
	}
}

func TestWithoutASessionPagesAndFormsDiffer(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := b.noFollow(mustGet(w.screens.URL + "/"))
	if p.status != 303 || !strings.HasPrefix(p.header.Get("Location"), "/signin?next=") {
		t.Fatalf("page: %d %v", p.status, p.header)
	}
	req := mustGet(w.screens.URL + "/")
	req.Header.Set("HX-Request", "true")
	p = b.noFollow(req)
	if p.status != 401 || p.header.Get("HX-Refresh") != "true" {
		t.Fatalf("htmx: %d %v", p.status, p.header)
	}
	// A cookie for a session that does not exist goes through a normal sign-in.
	req = mustGet(w.screens.URL + "/")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "invented"})
	p = b.noFollow(req)
	if p.status != 303 || !strings.HasPrefix(p.header.Get("Location"), "/signin/start") {
		t.Fatalf("stale: %d %v", p.status, p.header)
	}
}

func TestRenewsOnAPersonsRequestOnly(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	b.signIn(subject("renew"))
	if p := b.get("/"); p.status != 200 {
		t.Fatalf("%d", p.status)
	}
	if n := w.grants("refresh_token"); n != 0 {
		t.Fatalf("%d renewals while the token is fresh", n)
	}
	w.expireTokens()
	// A request on a timer does not renew, and does not count as the person.
	req := mustGet(w.screens.URL + "/")
	req.Header.Set("X-Timer-Request", "1")
	if p := b.do(req); p.status != 401 {
		t.Fatalf("timer: %d", p.status)
	}
	if n := w.grants("refresh_token"); n != 0 {
		t.Fatalf("a timer request renewed: %d", n)
	}
	// The person's own request does, once, however many come at once.
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p := b.get("/"); p.status != 200 {
				t.Errorf("%d", p.status)
			}
		}()
	}
	wg.Wait()
	if n := w.grants("refresh_token"); n != 1 {
		t.Fatalf("%d renewals; a token may be sent once", n)
	}
	w.expireTokens()
	if p := b.get("/"); p.status != 200 {
		t.Fatalf("second renewal: %d", p.status)
	}
	if n := w.grants("refresh_token"); n != 2 {
		t.Fatalf("%d renewals", n)
	}
	for _, r := range w.received("mjolnir") {
		if strings.Contains(r.Query, "prompt=none") {
			t.Fatal("prompt=none was sent")
		}
	}
	if strings.Contains(w.logs.String(), clientSecret) {
		t.Fatal("the client secret reached a log")
	}
}

func TestARefusedRenewalGoesThroughSignIn(t *testing.T) {
	for _, mode := range []string{"renew-invalid-grant", "renew-access-denied"} {
		t.Run(mode, func(t *testing.T) {
			w := newWorld(t)
			b := w.browser()
			b.signIn(subject("refused"))
			w.expireTokens()
			w.control("mjolnir", mode, nil)
			p := b.noFollow(mustGet(w.screens.URL + "/"))
			if p.status != 303 || p.header.Get("Location") != "/signin/start?next=%2F" {
				t.Fatalf("%d %v", p.status, p.header)
			}
			// MJOLNIR's session is live, so it passes with no form.
			w.control("mjolnir", "normal", nil)
			if p := b.get("/"); p.status != 200 || strings.Contains(p.body, "MJOLNIR stand-in") {
				t.Fatalf("%d %s", p.status, p.body)
			}
			req := mustGet(w.screens.URL + "/")
			req.Header.Set("HX-Request", "true")
			w.expireTokens()
			w.control("mjolnir", mode, nil)
			if p := b.noFollow(req); p.status != 401 || p.header.Get("HX-Refresh") != "true" {
				t.Fatalf("htmx: %d %v", p.status, p.header)
			}
		})
	}
}

func TestServerErrorLetsNobodyThroughAndKeepsTheSession(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	b.signIn(subject("trouble"))
	w.expireTokens()
	w.control("mjolnir", "renew-server-error", nil)
	p := b.get("/")
	if p.status != 503 || strings.Contains(p.body, "Test trouble") {
		t.Fatalf("%d", p.status)
	}
	w.control("mjolnir", "normal", nil)
	if p := b.get("/"); p.status != 200 || !strings.Contains(p.body, "Test trouble") {
		t.Fatalf("after it recovers: %d", p.status)
	}
}

func TestSignOutRevokesAndNeedsTheToken(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	who := subject("leaving")
	b.signIn(who)
	token := b.csrf()
	if p := b.post("/signout", url.Values{"csrf": {"wrong"}}); p.status != 403 {
		t.Fatalf("wrong token: %d", p.status)
	}
	if p := b.get("/"); p.status != 200 || !strings.Contains(p.body, "Test "+who) {
		t.Fatal("a refused sign-out ended the session")
	}
	countRevoked := func() int {
		revoked := 0
		for _, r := range w.received("mjolnir") {
			if strings.HasSuffix(r.Path, "/oauth2/revoke") {
				revoked++
				if r.Auth != "Basic" {
					t.Fatalf("not client_secret_basic: %+v", r)
				}
			}
		}
		return revoked
	}
	if n := countRevoked(); n != 0 {
		t.Fatalf("a refused sign-out revoked %d", n)
	}
	// The revocation has happened by the time the browser is told to go; nothing sent
	// changes where it goes.
	leave, _ := http.NewRequest(http.MethodPost, w.screens.URL+"/signout?next=https://evil.test/", strings.NewReader(url.Values{"csrf": {token}, "next": {"https://evil.test/"}}.Encode()))
	leave.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	leave.Header.Set("Referer", "https://evil.test/")
	p := b.noFollow(leave)
	if p.status != 303 || p.header.Get("Location") != w.standin.URL+"/mjolnir/sign-out" {
		t.Fatalf("%d %q", p.status, p.header.Get("Location"))
	}
	if n := countRevoked(); n != 1 {
		t.Fatalf("%d revocations when the browser was sent on", n)
	}
	req := mustGet(w.screens.URL + "/")
	if p := b.noFollow(req); p.status != 303 {
		t.Fatalf("still signed in: %d", p.status)
	}
	// MJOLNIR being down does not stop a person signing out of ROSETTA.
	b2 := w.browser()
	b2.signIn(subject("leaving"))
	tok := b2.csrf()
	w.control("mjolnir", "renew-server-error", nil)
	if p := b2.post("/signout", url.Values{"csrf": {tok}}); p.status != 200 || !strings.HasSuffix(p.url, "/mjolnir/sign-out") {
		t.Fatalf("%d %s", p.status, p.url)
	}
}

// The whole way round: MJOLNIR's page ends the sign-in, so the next one shows the form
// instead of passing with none.
func TestSignOutEndsTheSignInAtMjolnir(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	who := subject("leaving")
	b.signIn(who)
	if p := b.post("/signout", url.Values{"csrf": {b.csrf()}}); p.status != 200 || !strings.HasSuffix(p.url, "/mjolnir/sign-out") {
		t.Fatalf("%d %s", p.status, p.url)
	}
	if p := b.get("/signin/start"); !strings.Contains(p.body, "MJOLNIR stand-in") {
		t.Fatalf("passed with no form: %d %s", p.status, p.url)
	}
}

// A page with the sign-out form names the origins it passes through in form-action, and
// nothing else in the policy moves. A page without the form stays with 'self'.
func TestFormActionNamesWhereSignOutGoes(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	const rest = "default-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'; form-action %s; frame-ancestors 'none'; base-uri 'none'"
	if p := b.get("/signin"); p.header.Get("Content-Security-Policy") != fmt.Sprintf(rest, "'self'") {
		t.Fatalf("signed out: %q", p.header.Get("Content-Security-Policy"))
	}
	b.signIn(subject("policy"))
	p := b.get("/")
	if !strings.Contains(p.body, `action="/signout"`) {
		t.Fatal("no sign-out form on the page")
	}
	if got := p.header.Get("Content-Security-Policy"); got != fmt.Sprintf(rest, "'self' "+w.standin.URL) {
		t.Fatalf("signed in: %q", got)
	}
}

func TestOriginsOf(t *testing.T) {
	got := originsOf("https://sign-out.epc.test/sign-out?x=1", "https://sso.epc.test/", "https://sso.epc.test/other", "http://localhost:53190/mjolnir", "/nowhere", "")
	want := []string{"https://sign-out.epc.test", "https://sso.epc.test", "http://localhost:53190"}
	if !slices.Equal(got, want) {
		t.Fatalf("%v", got)
	}
}

// With no session there is nothing to revoke, and the browser still goes to MJOLNIR.
func TestSignOutWithNoSessionStillGoesToMjolnir(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	req, _ := http.NewRequest(http.MethodPost, w.screens.URL+"/signout", nil)
	p := b.noFollow(req)
	if p.status != 303 || p.header.Get("Location") != w.standin.URL+"/mjolnir/sign-out" {
		t.Fatalf("%d %q", p.status, p.header.Get("Location"))
	}
}

func TestSafeNextStaysHere(t *testing.T) {
	for next, want := range map[string]string{
		"/people":            "/people",
		"":                   "/",
		"//evil.example":     "/",
		"https://evil.test":  "/",
		"/\\evil.example":    "/",
		"/x\r\nSet-Cookie:a": "/",
	} {
		if got := SafeNext(next); got != want {
			t.Errorf("%q: %q", next, got)
		}
	}
}

// Nothing loads from outside: no outside address in any page or the style sheet,
// and the policy forbids it anyway (R-NFR-11).
func TestNothingFromOutsideAndUnder200KB(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	outside := regexp.MustCompile(`(?:src|href)="(?:https?:)?//|url\(["']?(?:https?:)?//|@import`)
	for _, path := range []string{"/signin", "/signin?next=/x", "/no-such-page"} {
		p := b.get(path)
		if outside.MatchString(p.body) {
			t.Errorf("%s loads from outside", path)
		}
		if csp := p.header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") || !strings.Contains(csp, "script-src 'self'") {
			t.Errorf("%s: %q", path, csp)
		}
		if len(p.body) > 200<<10 {
			t.Errorf("%s is %d bytes", path, len(p.body))
		}
	}
	css := b.get("/static/" + strings.TrimPrefix(StaticPrefix, "/static/") + "rosetta.css")
	if css.status != 200 || outside.MatchString(css.body) {
		t.Fatalf("%d", css.status)
	}
	for _, m := range regexp.MustCompile(`url\("([^"]+)"\)`).FindAllStringSubmatch(css.body, -1) {
		if p := b.get(StaticPrefix + m[1]); p.status != 200 {
			t.Errorf("font %s: %d", m[1], p.status)
		}
	}
	if p := b.get(StaticPrefix); p.status != 404 {
		t.Fatalf("a folder list: %d", p.status)
	}
	if c := b.get(StaticPrefix + "htmx.min.js"); c.status != 200 || !strings.Contains(c.body, "htmx") {
		t.Fatalf("htmx: %d", c.status)
	}
}

// Every word a screen names is a message.
func TestEveryScreenWordIsAMessage(t *testing.T) {
	entries, err := os.ReadDir("pages")
	if err != nil {
		t.Fatal(err)
	}
	word := regexp.MustCompile(`\bt (?:\$?\.[A-Za-z.]*|\$[a-z]+|"[a-z]+") "([A-Za-z0-9]+)"`)
	found := 0
	for _, e := range entries {
		raw, err := os.ReadFile("pages/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range word.FindAllStringSubmatch(string(raw), -1) {
			found++
			if !i18n.Has(m[1]) {
				t.Errorf("%s names %q", e.Name(), m[1])
			}
		}
	}
	if found == 0 {
		t.Fatal("found no words")
	}
}

func TestEveryScreenRendersInEachLanguage(t *testing.T) {
	w := newWorld(t)
	for _, l := range i18n.Langs {
		b := w.browser()
		req := mustGet(w.screens.URL + "/signin")
		req.Header.Set("Accept-Language", string(l))
		p := b.do(req)
		if p.status != 200 || !strings.Contains(p.body, `<html lang="`+string(l)+`">`) || !strings.Contains(p.body, i18n.T(l, "tagline")) {
			t.Errorf("%s: %d", l, p.status)
		}
	}
}
