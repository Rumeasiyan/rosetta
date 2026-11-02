// Package oidc signs people in through MJOLNIR, the provincial sign-in, with
// OpenID Connect's code flow and PKCE (R-ADM-01; B 8). Locally MJOLNIR is the
// stand-in in cmd/rosetta-standin; on the server it is MJOLNIR itself (E-ENB-08).
// Only the standard library is used.
//
// Renewal and revocation follow docs/SESSION-INTERFACE.md in MJOLNIR's
// repository: renew with the refresh token on a person's own request, never on a
// timer and never with prompt=none; send one refresh token only once; revoke it
// when the person signs out; authenticate as client_secret_basic.
package oidc

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config is the sign-in's settings.
type Config struct {
	Issuer       string // ROSETTA_SIGNIN_ISSUER, exactly as MJOLNIR gives it in its tokens
	InternalURL  string // ROSETTA_SIGNIN_INTERNAL_URL: where ROSETTA itself reaches it; empty means the issuer
	ClientID     string // ROSETTA_SIGNIN_CLIENT_ID
	ClientSecret string // ROSETTA_SIGNIN_CLIENT_SECRET: never log it
	RedirectURL  string // ROSETTA_PUBLIC_URL + /signin/callback
}

// CallbackPath is where MJOLNIR sends the person back to.
const CallbackPath = "/signin/callback"

// PublicURL reads ROSETTA_PUBLIC_URL: the address people reach the screens on.
// It must be https, except on the machine itself for local work.
func PublicURL(getenv func(string) string) (string, error) {
	public := strings.TrimRight(strings.TrimSpace(getenv("ROSETTA_PUBLIC_URL")), "/")
	if public == "" {
		return "", errors.New("ROSETTA_PUBLIC_URL is not set (see .env.example)")
	}
	u, err := url.Parse(public)
	if err != nil || u.Host == "" {
		return "", errors.New("ROSETTA_PUBLIC_URL is not an address")
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", errors.New("ROSETTA_PUBLIC_URL must be https, except on this machine for local work")
		}
	default:
		return "", errors.New("ROSETTA_PUBLIC_URL must be https")
	}
	return public, nil
}

// FromEnv reads the sign-in settings. Pass os.Getenv.
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		Issuer:       strings.TrimSpace(getenv("ROSETTA_SIGNIN_ISSUER")),
		InternalURL:  strings.TrimRight(strings.TrimSpace(getenv("ROSETTA_SIGNIN_INTERNAL_URL")), "/"),
		ClientID:     strings.TrimSpace(getenv("ROSETTA_SIGNIN_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(getenv("ROSETTA_SIGNIN_CLIENT_SECRET")),
	}
	for _, s := range []struct{ name, value string }{
		{"ROSETTA_SIGNIN_ISSUER", c.Issuer},
		{"ROSETTA_SIGNIN_CLIENT_ID", c.ClientID},
		{"ROSETTA_SIGNIN_CLIENT_SECRET", c.ClientSecret},
	} {
		if s.value == "" {
			return c, fmt.Errorf("%s is not set (sign-in through MJOLNIR; see .env.example)", s.name)
		}
	}
	for _, s := range []struct{ name, value string }{
		{"ROSETTA_SIGNIN_ISSUER", c.Issuer},
		{"ROSETTA_SIGNIN_INTERNAL_URL", c.InternalURL},
	} {
		if s.value == "" {
			continue
		}
		if u, err := url.Parse(s.value); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return c, fmt.Errorf("%s is not an http or https address", s.name)
		}
	}
	public, err := PublicURL(getenv)
	if err != nil {
		return c, err
	}
	c.RedirectURL = public + CallbackPath
	return c, nil
}

// Claims are what ROSETTA takes from a verified ID token.
type Claims struct {
	Subject string
	Name    string
	Email   string
}

// Tokens is one answer from MJOLNIR's token address.
type Tokens struct {
	Claims  Claims    // empty when a renewal brought no ID token
	Refresh string    // empty when none was given: the person signs in again at Expires
	Expires time.Time // when a person's request must next renew
}

// ErrRefused says MJOLNIR refused a renewal (invalid_grant or access_denied):
// the session is over, and the person goes through a normal sign-in. Any other
// error means "try again": nobody is let through, and the session is kept.
var ErrRefused = errors.New("sign-in renewal refused")

// Provider talks to one issuer. Discovery and keys are fetched when first needed
// and kept; an unknown signing key triggers one refetch.
type Provider struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu   sync.Mutex
	meta *metadata
	keys map[string]crypto.PublicKey
}

type metadata struct {
	Issuer    string `json:"issuer"`
	AuthURL   string `json:"authorization_endpoint"`
	TokenURL  string `json:"token_endpoint"`
	RevokeURL string `json:"revocation_endpoint"`
	JWKSURL   string `json:"jwks_uri"`
}

// New makes a provider. client may be nil.
func New(cfg Config, client *http.Client) *Provider {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{cfg: cfg, client: client, now: time.Now}
}

// Pending is one sign-in on its way: kept by ROSETTA until MJOLNIR sends the
// person back.
type Pending struct {
	State    string
	Nonce    string
	Verifier string
}

// NewPending makes fresh random values for one sign-in.
func NewPending() Pending {
	return Pending{State: Random(), Nonce: Random(), Verifier: Random()}
}

// Random is 32 random bytes, base64url without padding.
func Random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // the system has no randomness: nothing safe can go on
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// AuthURL is where to send the person to sign in. It never carries prompt=none.
func (p *Provider) AuthURL(ctx context.Context, pd Pending) (string, error) {
	m, err := p.metadata(ctx)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(pd.Verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.cfg.ClientID},
		"redirect_uri":          {p.cfg.RedirectURL},
		"scope":                 {"openid profile email offline_access"},
		"state":                 {pd.State},
		"nonce":                 {pd.Nonce},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	sep := "?"
	if strings.Contains(m.AuthURL, "?") {
		sep = "&"
	}
	return m.AuthURL + sep + q.Encode(), nil
}

// Exchange trades the code MJOLNIR returned for tokens and verifies the ID token.
func (p *Provider) Exchange(ctx context.Context, code string, pd Pending) (Tokens, error) {
	status, body, err := p.tokenCall(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.cfg.RedirectURL},
		"code_verifier": {pd.Verifier},
	})
	if err != nil {
		return Tokens{}, err
	}
	if status != http.StatusOK {
		return Tokens{}, fmt.Errorf("sign-in token request: status %d", status)
	}
	var tok tokenAnswer
	if err := json.Unmarshal(body, &tok); err != nil || tok.IDToken == "" {
		return Tokens{}, errors.New("sign-in token answer has no ID token")
	}
	claims, exp, err := p.verify(ctx, tok.IDToken, pd.Nonce)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{Claims: claims, Refresh: tok.Refresh, Expires: tok.expires(p.now(), exp)}, nil
}

// Renew sends one refresh token, once. A new ID token is held to the person
// the session belongs to. See ErrRefused for what its error means.
func (p *Provider) Renew(ctx context.Context, refresh, nonce, subject string) (Tokens, error) {
	status, body, err := p.tokenCall(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	if err != nil {
		return Tokens{}, err
	}
	if status != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && (e.Error == "invalid_grant" || e.Error == "access_denied") {
			return Tokens{}, ErrRefused
		}
		return Tokens{}, fmt.Errorf("sign-in renewal: status %d", status)
	}
	var tok tokenAnswer
	if err := json.Unmarshal(body, &tok); err != nil {
		return Tokens{}, errors.New("sign-in renewal answer is not JSON")
	}
	out := Tokens{Refresh: tok.Refresh}
	exp := time.Time{}
	if tok.IDToken != "" {
		var claims Claims
		if claims, exp, err = p.verify(ctx, tok.IDToken, nonce); err != nil {
			return Tokens{}, err
		}
		if claims.Subject != subject {
			return Tokens{}, errors.New("sign-in renewal names another person")
		}
		out.Claims = claims
	}
	out.Expires = tok.expires(p.now(), exp)
	return out, nil
}

// Revoke ends a refresh token, when the person signs out. Nothing else stops it
// (SESSION-INTERFACE.md).
func (p *Provider) Revoke(ctx context.Context, refresh string) error {
	m, err := p.metadata(ctx)
	if err != nil {
		return err
	}
	if m.RevokeURL == "" {
		return errors.New("the sign-in service names no revocation address")
	}
	status, _, err := p.post(ctx, m.RevokeURL, url.Values{"token": {refresh}, "token_type_hint": {"refresh_token"}})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("sign-in revocation: status %d", status)
	}
	return nil
}

type tokenAnswer struct {
	IDToken   string `json:"id_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int64  `json:"expires_in"`
}

// expires is when the access token ends: what MJOLNIR said, else the ID token's
// end, else now, so the next request asks again.
func (t tokenAnswer) expires(now, idExpiry time.Time) time.Time {
	if t.ExpiresIn > 0 {
		return now.Add(time.Duration(t.ExpiresIn) * time.Second)
	}
	if !idExpiry.IsZero() {
		return idExpiry
	}
	return now
}

func (p *Provider) tokenCall(ctx context.Context, form url.Values) (int, []byte, error) {
	m, err := p.metadata(ctx)
	if err != nil {
		return 0, nil, err
	}
	return p.post(ctx, m.TokenURL, form)
}

// post sends a form as client_secret_basic, the only way MJOLNIR tests.
func (p *Provider) post(ctx context.Context, address string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(p.cfg.ClientID), url.QueryEscape(p.cfg.ClientSecret))
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, errors.New("the sign-in service could not be reached")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, body, nil
}

// Verify checks an ID token's signature, issuer, audience, times and nonce.
func (p *Provider) Verify(ctx context.Context, idToken, nonce string) (Claims, error) {
	c, _, err := p.verify(ctx, idToken, nonce)
	return c, err
}

func (p *Provider) verify(ctx context.Context, idToken, nonce string) (Claims, time.Time, error) {
	var none time.Time
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Claims{}, none, errors.New("ID token is not a signed token")
	}
	var head struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodePart(parts[0], &head); err != nil {
		return Claims{}, none, err
	}
	key, err := p.key(ctx, head.Kid)
	if err != nil {
		return Claims{}, none, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, none, errors.New("ID token signature is not base64url")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	switch k := key.(type) {
	case *rsa.PublicKey:
		if head.Alg != "RS256" || rsa.VerifyPKCS1v15(k, crypto.SHA256, digest[:], sig) != nil {
			return Claims{}, none, errors.New("ID token signature does not match")
		}
	case *ecdsa.PublicKey:
		if head.Alg != "ES256" || len(sig) != 64 ||
			!ecdsa.Verify(k, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			return Claims{}, none, errors.New("ID token signature does not match")
		}
	default:
		return Claims{}, none, errors.New("ID token uses an unsupported key")
	}
	var c struct {
		Iss   string          `json:"iss"`
		Sub   string          `json:"sub"`
		Aud   json.RawMessage `json:"aud"`
		Exp   int64           `json:"exp"`
		Iat   int64           `json:"iat"`
		Nonce string          `json:"nonce"`
		Name  string          `json:"name"`
		Email string          `json:"email"`
	}
	if err := decodePart(parts[1], &c); err != nil {
		return Claims{}, none, err
	}
	now := p.now()
	const skew = 2 * time.Minute
	switch {
	case c.Iss != p.cfg.Issuer:
		return Claims{}, none, errors.New("ID token is from another issuer")
	case !audienceHas(c.Aud, p.cfg.ClientID):
		return Claims{}, none, errors.New("ID token is for another client")
	case c.Exp == 0 || now.After(time.Unix(c.Exp, 0).Add(skew)):
		return Claims{}, none, errors.New("ID token has expired")
	case c.Iat != 0 && time.Unix(c.Iat, 0).After(now.Add(skew)):
		return Claims{}, none, errors.New("ID token is from the future")
	case c.Nonce != nonce:
		return Claims{}, none, errors.New("ID token does not belong to this sign-in")
	case strings.TrimSpace(c.Sub) == "" || len(c.Sub) > 200:
		return Claims{}, none, errors.New("ID token has no usable subject")
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		name = c.Sub
	}
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	return Claims{Subject: c.Sub, Name: name, Email: strings.TrimSpace(c.Email)}, time.Unix(c.Exp, 0), nil
}

func audienceHas(raw json.RawMessage, id string) bool {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return one == id
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		for _, a := range many {
			if a == id {
				return true
			}
		}
	}
	return false
}

func decodePart(part string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(part)
	if err != nil {
		return errors.New("ID token part is not base64url")
	}
	if err := json.Unmarshal(b, v); err != nil {
		return errors.New("ID token part is not JSON")
	}
	return nil
}

func (p *Provider) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return errors.New("the sign-in service could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the sign-in service answered status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

// metadata is the issuer's discovery document. Where ROSETTA has an inside way
// to MJOLNIR (B 8), the addresses it calls itself move there; the address the
// browser is sent to stays as published, and the issuer is compared as published.
func (p *Provider) metadata(ctx context.Context) (*metadata, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.meta != nil {
		return p.meta, nil
	}
	published := strings.TrimRight(p.cfg.Issuer, "/")
	base := published
	if p.cfg.InternalURL != "" {
		base = p.cfg.InternalURL
	}
	var m metadata
	if err := p.getJSON(ctx, base+"/.well-known/openid-configuration", &m); err != nil {
		return nil, fmt.Errorf("sign-in discovery: %w", err)
	}
	if m.Issuer != p.cfg.Issuer || m.AuthURL == "" || m.TokenURL == "" || m.JWKSURL == "" {
		return nil, errors.New("sign-in discovery: the issuer's details do not match")
	}
	if base != published {
		for _, a := range []*string{&m.TokenURL, &m.RevokeURL, &m.JWKSURL} {
			if rest, ok := strings.CutPrefix(*a, published); ok {
				*a = base + rest
			}
		}
	}
	p.meta = &m
	return p.meta, nil
}

// key finds the signing key kid, fetching the issuer's keys again once if it is
// unknown (keys are replaced from time to time).
func (p *Provider) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	m, err := p.metadata(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	k, ok := p.keys[kid]
	p.mu.Unlock()
	if ok {
		return k, nil
	}
	var set struct {
		Keys []struct {
			Kty, Kid, Use, N, E, Crv, X, Y string
		} `json:"keys"`
	}
	if err := p.getJSON(ctx, m.JWKSURL, &set); err != nil {
		return nil, fmt.Errorf("sign-in keys: %w", err)
	}
	keys := map[string]crypto.PublicKey{}
	for _, j := range set.Keys {
		if j.Use != "" && j.Use != "sig" {
			continue
		}
		switch j.Kty {
		case "RSA":
			n, err1 := base64.RawURLEncoding.DecodeString(j.N)
			e, err2 := base64.RawURLEncoding.DecodeString(j.E)
			if err1 != nil || err2 != nil || len(n) < 256 {
				continue
			}
			keys[j.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		case "EC":
			x, err1 := base64.RawURLEncoding.DecodeString(j.X)
			y, err2 := base64.RawURLEncoding.DecodeString(j.Y)
			if err1 != nil || err2 != nil || j.Crv != "P-256" {
				continue
			}
			keys[j.Kid] = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		}
	}
	p.mu.Lock()
	p.keys = keys
	p.mu.Unlock()
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	return nil, errors.New("ID token is signed with an unknown key")
}
