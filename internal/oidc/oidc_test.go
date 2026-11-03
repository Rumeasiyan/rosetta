package oidc_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/oidc"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

const secret = "local-stand-in-only"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestFromEnvNamesWhatIsMissing(t *testing.T) {
	all := map[string]string{
		"ROSETTA_SIGNIN_ISSUER":        "https://sso.epc.test/",
		"ROSETTA_SIGNIN_CLIENT_ID":     "rosetta",
		"ROSETTA_SIGNIN_CLIENT_SECRET": "invented",
		"ROSETTA_PUBLIC_URL":           "https://rosetta.epc.test/",
	}
	c, err := oidc.FromEnv(env(all))
	if err != nil {
		t.Fatal(err)
	}
	if c.Issuer != "https://sso.epc.test/" {
		t.Fatalf("the issuer is kept exactly as published: %q", c.Issuer)
	}
	if c.RedirectURL != "https://rosetta.epc.test/signin/callback" {
		t.Fatal(c.RedirectURL)
	}
	for name := range all {
		less := map[string]string{}
		for k, v := range all {
			if k != name {
				less[k] = v
			}
		}
		if _, err := oidc.FromEnv(env(less)); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: %v", name, err)
		}
	}
}

func TestSignOutURLIsHTTPSExceptOnThisMachine(t *testing.T) {
	for address, ok := range map[string]bool{
		"https://sso.epc.test/sign-out":           true,
		"http://localhost:53190/mjolnir/sign-out": true,
		"http://127.0.0.1:53190/mjolnir/sign-out": true,
		"http://sso.epc.test/sign-out":            false,
		"ftp://sso.epc.test/sign-out":             false,
		"/sign-out":                               false,
		"":                                        false,
	} {
		got, err := oidc.SignOutURL(env(map[string]string{"ROSETTA_SIGNOUT_URL": address}))
		if (err == nil) != ok || (ok && got != address) {
			t.Errorf("%q: %q %v", address, got, err)
		}
		if !ok && !strings.Contains(err.Error(), "ROSETTA_SIGNOUT_URL") {
			t.Errorf("%q: the error does not name the setting: %v", address, err)
		}
	}
}

func TestPublicURLIsHTTPSExceptOnThisMachine(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://rosetta.epc.test": true,
		"http://localhost:53080":   true,
		"http://127.0.0.1:53080":   true,
		"http://rosetta.epc.lk":    false,
		"ftp://rosetta.epc.lk":     false,
		"rosetta.epc.lk":           false,
		"":                         false,
	} {
		if _, err := oidc.PublicURL(env(map[string]string{"ROSETTA_PUBLIC_URL": u})); (err == nil) != ok {
			t.Errorf("%q: %v", u, err)
		}
	}
}

// setup runs a provider against MJOLNIR's stand-in.
func setup(t *testing.T) (*oidc.Provider, *httptest.Server) {
	t.Helper()
	s := standin.New(standin.Options{ClientID: "rosetta", ClientSecret: secret})
	ts := httptest.NewServer(s.Handler())
	s.SetPublicURL(ts.URL)
	t.Cleanup(func() { s.Close(); ts.Close() })
	p := oidc.New(oidc.Config{Issuer: ts.URL + "/mjolnir", ClientID: "rosetta", ClientSecret: secret, RedirectURL: "http://localhost/cb"}, nil)
	return p, ts
}

// signIn walks the stand-in's form as a browser would, keeping nothing.
func signIn(t *testing.T, p *oidc.Provider, ts *httptest.Server, pd oidc.Pending) string {
	t.Helper()
	auth, err := p.AuthURL(context.Background(), pd)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(auth)
	q := u.Query()
	if q.Get("prompt") != "" {
		t.Fatalf("prompt sent: %s", auth)
	}
	if !strings.Contains(q.Get("scope"), "offline_access") {
		t.Fatalf("no offline_access: %s", auth)
	}
	sum := sha256.Sum256([]byte(pd.Verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatal("wrong challenge")
	}
	q.Set("account", "dev-admin")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.PostForm(ts.URL+"/mjolnir/oauth2/auth", q)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return loc.Query().Get("code")
}

func TestExchangeRenewAndRevoke(t *testing.T) {
	p, ts := setup(t)
	ctx := context.Background()
	pd := oidc.NewPending()
	got, err := p.Exchange(ctx, signIn(t, p, ts, pd), pd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Claims.Subject != "dev-admin" || got.Claims.Name != "Test Administrator" || got.Claims.Email != "dev-admin@example.invalid" {
		t.Fatalf("%+v", got.Claims)
	}
	if got.Refresh == "" || time.Until(got.Expires) < time.Minute {
		t.Fatalf("%+v", got)
	}
	again, err := p.Renew(ctx, got.Refresh, pd.Nonce, "dev-admin")
	if err != nil {
		t.Fatal(err)
	}
	if again.Refresh == "" || again.Refresh == got.Refresh {
		t.Fatal("a renewal gives a new refresh token")
	}
	if _, err := p.Renew(ctx, again.Refresh, pd.Nonce, "somebody-else"); err == nil {
		t.Fatal("a renewal naming another person was taken")
	}
	// That token was sent, so it may not be sent again; a new sign-in is needed.
	pd2 := oidc.NewPending()
	third, err := p.Exchange(ctx, signIn(t, p, ts, pd2), pd2)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Revoke(ctx, third.Refresh); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Renew(ctx, third.Refresh, pd2.Nonce, "dev-admin"); !errors.Is(err, oidc.ErrRefused) {
		t.Fatalf("after revoke: %v", err)
	}
}

func TestExchangeRefusesAWrongNonceOrCode(t *testing.T) {
	p, ts := setup(t)
	ctx := context.Background()
	pd := oidc.NewPending()
	code := signIn(t, p, ts, pd)
	bad := pd
	bad.Nonce = "another"
	if _, err := p.Exchange(ctx, code, bad); err == nil {
		t.Fatal("a token from another sign-in was taken")
	}
	if _, err := p.Exchange(ctx, "no-such-code", pd); err == nil {
		t.Fatal("no such code was taken")
	}
	pd3 := oidc.NewPending()
	code3 := signIn(t, p, ts, pd3)
	wrong := pd3
	wrong.Verifier = "another"
	if _, err := p.Exchange(ctx, code3, wrong); err == nil {
		t.Fatal("PKCE was not held to")
	}
}

// Only invalid_grant and access_denied end a session; anything else is "try again".
func TestRenewTellsRefusedFromTryAgain(t *testing.T) {
	for name, c := range map[string]struct {
		status  int
		body    string
		refused bool
	}{
		"rotated":      {400, `{"error":"invalid_grant"}`, true},
		"switched off": {403, `{"error":"access_denied"}`, true},
		"server error": {500, `{"error":"server_error"}`, false},
		"gateway":      {502, `not json`, false},
		"bad request":  {400, `{"error":"invalid_request"}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			var ts *httptest.Server
			ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "openid-configuration") {
					io.WriteString(w, `{"issuer":"`+ts.URL+`","authorization_endpoint":"`+ts.URL+`/a","token_endpoint":"`+ts.URL+`/t","jwks_uri":"`+ts.URL+`/j"}`)
					return
				}
				w.WriteHeader(c.status)
				io.WriteString(w, c.body)
			}))
			defer ts.Close()
			p := oidc.New(oidc.Config{Issuer: ts.URL, ClientID: "c", ClientSecret: secret}, nil)
			_, err := p.Renew(context.Background(), "tok", "n", "s")
			if err == nil || errors.Is(err, oidc.ErrRefused) != c.refused {
				t.Fatalf("%v", err)
			}
			if strings.Contains(err.Error(), "tok") || strings.Contains(err.Error(), secret) {
				t.Fatalf("an error holds a secret: %v", err)
			}
		})
	}
}

// ROSETTA may reach MJOLNIR by an inside address, while the browser and the
// issuer's name stay as published.
func TestInternalAddressKeepsThePublishedIssuer(t *testing.T) {
	var inside *httptest.Server
	inside = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "openid-configuration") {
			io.WriteString(w, `{"issuer":"https://sso.epc.test/","authorization_endpoint":"https://sso.epc.test/oauth2/auth",
				"token_endpoint":"https://sso.epc.test/oauth2/token","jwks_uri":"https://sso.epc.test/keys"}`)
			return
		}
		if r.URL.Path == "/oauth2/token" {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		t.Errorf("called %s", r.URL.Path)
	}))
	defer inside.Close()
	p := oidc.New(oidc.Config{Issuer: "https://sso.epc.test/", InternalURL: inside.URL, ClientID: "c", ClientSecret: secret, RedirectURL: "http://localhost/cb"}, nil)
	auth, err := p.AuthURL(context.Background(), oidc.NewPending())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(auth, "https://sso.epc.test/oauth2/auth?") {
		t.Fatalf("the browser goes to the published address: %s", auth)
	}
	if _, err := p.Renew(context.Background(), "t", "n", "s"); !errors.Is(err, oidc.ErrRefused) {
		t.Fatalf("the token call stays inside: %v", err)
	}
}
