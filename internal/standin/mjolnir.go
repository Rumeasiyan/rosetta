package standin

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Modes after normal: refuse renewals each way SESSION-INTERFACE.md describes,
// always show the form (a new person each time), or stop answering.
var mjolnirModes = []string{"normal", "form-always", "renew-invalid-grant", "renew-access-denied", "renew-server-error", "slow", "silent"}

// Account is an invented person MJOLNIR's stand-in signs in.
type Account struct{ Subject, Name, Email string }

// Accounts is who the form offers. Any other subject typed there is signed in too.
var Accounts = []Account{
	{"dev-admin", "Test Administrator", "dev-admin@example.invalid"},
	{"dev-keeper", "Test Keeper", "dev-keeper@example.invalid"},
	{"dev-owner", "Test Owner", "dev-owner@example.invalid"},
	{"dev-officer", "Test Officer", "dev-officer@example.invalid"},
}

const mjolnirCookie = "standin_mjolnir"

type mjCode struct {
	account   Account
	nonce     string
	redirect  string
	challenge string
	scope     string
	expires   time.Time
}

type mjRefresh struct {
	account Account
	nonce   string
	chain   string
	used    bool
}

type mjolnirState struct {
	client, secret string
	key            *ecdsa.PrivateKey

	mu       sync.Mutex
	codes    map[string]mjCode
	refresh  map[string]*mjRefresh
	dead     map[string]bool // chains ended by reuse or revocation
	sessions map[string]Account
}

func newMjolnir(client, secret string) *mjolnirState {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return &mjolnirState{client: client, secret: secret, key: key}
}

func (m *mjolnirState) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes, m.refresh, m.dead, m.sessions = map[string]mjCode{}, map[string]*mjRefresh{}, map[string]bool{}, map[string]Account{}
}

func randomText() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) issuer() string { return s.public + "/mjolnir" }

func (s *Server) mountMjolnir(mux *http.ServeMux) {
	mux.HandleFunc("GET /mjolnir/.well-known/openid-configuration", s.serve("mjolnir", s.mjDiscovery))
	mux.HandleFunc("GET /mjolnir/.well-known/jwks.json", s.serve("mjolnir", s.mjKeys))
	mux.HandleFunc("GET /mjolnir/oauth2/auth", s.serve("mjolnir", s.mjAuth))
	mux.HandleFunc("POST /mjolnir/oauth2/auth", s.serve("mjolnir", s.mjForm))
	mux.HandleFunc("POST /mjolnir/oauth2/token", s.serve("mjolnir", s.mjToken))
	mux.HandleFunc("POST /mjolnir/oauth2/revoke", s.serve("mjolnir", s.mjRevoke))
}

func (s *Server) mjDiscovery(w http.ResponseWriter, _ *http.Request) {
	i := s.issuer()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                 i,
		"authorization_endpoint": i + "/oauth2/auth",
		"token_endpoint":         i + "/oauth2/token",
		"revocation_endpoint":    i + "/oauth2/revoke",
		"jwks_uri":               i + "/.well-known/jwks.json",
	})
}

func (s *Server) mjKeys(w http.ResponseWriter, _ *http.Request) {
	pub := s.mjolnir.key.PublicKey
	ecdh, err := pub.ECDH()
	if err != nil {
		http.Error(w, "no key", http.StatusInternalServerError)
		return
	}
	raw := ecdh.Bytes() // 0x04, then X and Y of 32 bytes each
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kty": "EC", "use": "sig", "alg": "ES256", "crv": "P-256", "kid": "standin",
		"x": base64.RawURLEncoding.EncodeToString(raw[1:33]),
		"y": base64.RawURLEncoding.EncodeToString(raw[33:65]),
	}}})
}

// mjForward lists the parameters a sign-in carries from the first request to the form.
var mjForward = []string{"client_id", "redirect_uri", "state", "nonce", "scope", "code_challenge", "code_challenge_method"}

func (s *Server) mjAuth(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != s.mjolnir.client || q.Get("redirect_uri") == "" {
		http.Error(w, "invalid_client", http.StatusBadRequest)
		return
	}
	if q.Get("response_type") != "code" || q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		mjBack(w, r, q.Get("redirect_uri"), url.Values{"error": {"invalid_request"}, "state": {q.Get("state")}})
		return
	}
	// As MJOLNIR measured it, with or without a live session (D-016).
	if q.Get("prompt") == "none" {
		mjBack(w, r, q.Get("redirect_uri"), url.Values{"error": {"login_required"}, "state": {q.Get("state")}})
		return
	}
	mode, _ := s.mode("mjolnir")
	if c, err := r.Cookie(mjolnirCookie); err == nil && mode != "form-always" && q.Get("prompt") != "login" {
		s.mjolnir.mu.Lock()
		a, ok := s.mjolnir.sessions[c.Value]
		s.mjolnir.mu.Unlock()
		if ok {
			s.mjGrant(w, r, q, a)
			return
		}
	}
	data := struct {
		Fields   map[string]string
		Accounts []Account
	}{map[string]string{}, Accounts}
	for _, k := range mjForward {
		data.Fields[k] = q.Get(k)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	mjFormPage.Execute(w, data)
}

func (s *Server) mjForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("client_id") != s.mjolnir.client {
		http.Error(w, "invalid_request", http.StatusBadRequest)
		return
	}
	subject := strings.TrimSpace(r.PostForm.Get("own"))
	if subject == "" {
		subject = r.PostForm.Get("account")
	}
	if subject == "" {
		http.Error(w, "choose an account", http.StatusBadRequest)
		return
	}
	a := Account{Subject: subject, Name: "Test " + subject, Email: subject + "@example.invalid"}
	for _, known := range Accounts {
		if known.Subject == subject {
			a = known
		}
	}
	tok := randomText()
	s.mjolnir.mu.Lock()
	s.mjolnir.sessions[tok] = a
	s.mjolnir.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: mjolnirCookie, Value: tok, Path: "/mjolnir/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	s.mjGrant(w, r, r.PostForm, a)
}

func (s *Server) mjGrant(w http.ResponseWriter, r *http.Request, q url.Values, a Account) {
	code := randomText()
	s.mjolnir.mu.Lock()
	s.mjolnir.codes[code] = mjCode{account: a, nonce: q.Get("nonce"), redirect: q.Get("redirect_uri"),
		challenge: q.Get("code_challenge"), scope: q.Get("scope"), expires: time.Now().Add(time.Minute)}
	s.mjolnir.mu.Unlock()
	mjBack(w, r, q.Get("redirect_uri"), url.Values{"code": {code}, "state": {q.Get("state")}})
}

func mjBack(w http.ResponseWriter, r *http.Request, redirect string, q url.Values) {
	sep := "?"
	if strings.Contains(redirect, "?") {
		sep = "&"
	}
	http.Redirect(w, r, redirect+sep+q.Encode(), http.StatusSeeOther)
}

// mjClient checks client_secret_basic. A secret in the form is refused, as on
// the real service (SESSION-INTERFACE.md).
func (s *Server) mjClient(w http.ResponseWriter, r *http.Request) bool {
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	}
	if !ok || r.PostForm.Has("client_secret") || id != s.mjolnir.client ||
		subtle.ConstantTimeCompare([]byte(secret), []byte(s.mjolnir.secret)) != 1 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return false
	}
	return true
}

func (s *Server) mjToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if !s.mjClient(w, r) {
		return
	}
	mode, p := s.mode("mjolnir")
	ttl := intParam(p, "token_seconds", 300)
	m := s.mjolnir
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		m.mu.Lock()
		c, ok := m.codes[r.PostForm.Get("code")]
		delete(m.codes, r.PostForm.Get("code"))
		m.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || time.Now().After(c.expires) || c.redirect != r.PostForm.Get("redirect_uri") ||
			c.challenge != base64.RawURLEncoding.EncodeToString(sum[:]) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		refresh := ""
		if strings.Contains(" "+c.scope+" ", " offline_access ") {
			refresh = s.mjNewRefresh(c.account, c.nonce, randomText())
		}
		s.mjAnswer(w, c.account, c.nonce, refresh, ttl)
	case "refresh_token":
		switch mode {
		case "renew-invalid-grant":
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		case "renew-access-denied":
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "access_denied"})
			return
		case "renew-server-error":
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
			return
		}
		m.mu.Lock()
		rt, ok := m.refresh[r.PostForm.Get("refresh_token")]
		switch {
		case !ok || m.dead[rt.chain]:
			m.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		case rt.used:
			// Reusing a rotated token kills the newest in the chain too.
			m.dead[rt.chain] = true
			m.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		rt.used = true
		a, nonce, chain := rt.account, rt.nonce, rt.chain
		m.mu.Unlock()
		s.mjAnswer(w, a, nonce, s.mjNewRefresh(a, nonce, chain), ttl)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

func (s *Server) mjNewRefresh(a Account, nonce, chain string) string {
	tok := randomText()
	s.mjolnir.mu.Lock()
	s.mjolnir.refresh[tok] = &mjRefresh{account: a, nonce: nonce, chain: chain}
	s.mjolnir.mu.Unlock()
	return tok
}

func (s *Server) mjAnswer(w http.ResponseWriter, a Account, nonce, refresh string, ttl int) {
	now := time.Now()
	id, err := s.mjSign(map[string]any{
		"iss": s.issuer(), "sub": a.Subject, "aud": []string{s.mjolnir.client},
		"iat": now.Unix(), "exp": now.Add(time.Duration(ttl) * time.Second).Unix(),
		"nonce": nonce, "name": a.Name, "email": a.Email,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "server_error"})
		return
	}
	out := map[string]any{"access_token": randomText(), "token_type": "bearer", "expires_in": ttl, "id_token": id}
	if refresh != "" {
		out["refresh_token"] = refresh
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) mjSign(claims map[string]any) (string, error) {
	head, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": "standin", "typ": "JWT"})
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(head) + "." + enc.EncodeToString(body)
	sum := sha256.Sum256([]byte(input))
	x, y, err := ecdsa.Sign(rand.Reader, s.mjolnir.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	x.FillBytes(sig[:32])
	y.FillBytes(sig[32:])
	return input + "." + enc.EncodeToString(sig), nil
}

func (s *Server) mjRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if !s.mjClient(w, r) {
		return
	}
	m := s.mjolnir
	m.mu.Lock()
	if rt, ok := m.refresh[r.PostForm.Get("token")]; ok {
		m.dead[rt.chain] = true
	}
	m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

var mjFormPage = template.Must(template.New("form").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>MJOLNIR stand-in</title>
<style>body{font:16px system-ui,sans-serif;max-width:32rem;margin:2rem auto;padding:0 1rem}label{display:block;min-height:44px;padding:8px 0}input[type=text]{font:inherit;width:100%;box-sizing:border-box;min-height:44px;padding:0 8px}button{font:inherit;min-height:44px;padding:0 20px;margin-top:12px}</style>
</head><body>
<h1>MJOLNIR stand-in</h1>
<p>For development only. Every account here is invented.</p>
<form method="post" action="">
{{range $k, $v := .Fields}}<input type="hidden" name="{{$k}}" value="{{$v}}">
{{end}}{{range .Accounts}}<label><input type="radio" name="account" value="{{.Subject}}"> {{.Name}} ({{.Subject}})</label>
{{end}}<label for="own">Or another invented account</label><input type="text" id="own" name="own" autocomplete="off">
<button type="submit">Sign in</button>
</form></body></html>
`))
