package apisign

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	prefix = "rst_0123456789ab"
	nonce  = "a-nonce-of-sixteen-or-more"
)

var t0 = time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func signed(body []byte, target string, key ed25519.PrivateKey, at time.Time, n string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	Sign(r, body, prefix, n, key, at)
	return r
}

func verifier() Verifier {
	return Verifier{Nonces: &Nonces{}, Now: func() time.Time { return t0 }}
}

func TestAGoodSignaturePasses(t *testing.T) {
	pub, priv := newKey(t)
	body := []byte(`{"passages":["An invented letter."]}`)
	if err := verifier().Verify(signed(body, "/api/v1/translate", priv, t0, nonce), body, prefix, pub); err != nil {
		t.Fatal(err)
	}
	// An empty body is signed with the digest of zero bytes.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/gate", nil)
	Sign(r, nil, prefix, nonce, priv, t0)
	if r.Header.Get("Content-Digest") != "sha-256=:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=:" {
		t.Fatal(r.Header.Get("Content-Digest"))
	}
	if err := verifier().Verify(r, nil, prefix, pub); err != nil {
		t.Fatal(err)
	}
}

func TestTheSignedTextIsTheProfile(t *testing.T) {
	got := string(base("post", "/api/v1/translate", "sha-256=:x:", params(1793496600, prefix, nonce)))
	want := `"@method": POST
"@path": /api/v1/translate
"content-digest": sha-256=:x:
"@signature-params": ("@method" "@path" "content-digest");created=1793496600;keyid="rst_0123456789ab";nonce="a-nonce-of-sixteen-or-more";alg="ed25519";tag="caller-to-rosetta"`
	if got != want {
		t.Fatal(got)
	}
}

func TestRefusals(t *testing.T) {
	pub, priv := newKey(t)
	_, otherPriv := newKey(t)
	body := []byte(`{"a":1}`)
	good := func() *http.Request { return signed(body, "/api/v1/translate", priv, t0, nonce) }
	for name, c := range map[string]struct {
		r    *http.Request
		body []byte
		key  ed25519.PublicKey
		want error
	}{
		"no headers":      {httptest.NewRequest(http.MethodPost, "/api/v1/translate", nil), body, pub, ErrMissing},
		"a query":         {signed(body, "/api/v1/translate?x=1", priv, t0, nonce), body, pub, ErrQuery},
		"an empty query":  {signed(body, "/api/v1/translate?", priv, t0, nonce), body, pub, ErrQuery},
		"old":             {signed(body, "/api/v1/translate", priv, t0.Add(-Window-time.Second), nonce), body, pub, ErrExpired},
		"ahead":           {signed(body, "/api/v1/translate", priv, t0.Add(Window+time.Second), nonce), body, pub, ErrExpired},
		"another body":    {good(), []byte(`{"a":2}`), pub, ErrDigest},
		"another's key":   {signed(body, "/api/v1/translate", otherPriv, t0, nonce), body, pub, ErrBadSignature},
		"no public key":   {good(), body, nil, ErrBadSignature},
		"short nonce":     {signed(body, "/api/v1/translate", priv, t0, "short"), body, pub, ErrMissing},
		"strange nonce":   {signed(body, "/api/v1/translate", priv, t0, `a"nonce"of-sixteen-or-more`), body, pub, ErrMissing},
		"strange nonce 2": {signed(body, "/api/v1/translate", priv, t0, "a nonce of sixteen or more"), body, pub, ErrMissing},
	} {
		if err := verifier().Verify(c.r, c.body, prefix, c.key); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Moved to another path or method, the signature no longer fits.
	moved := good()
	moved.URL.Path = "/api/v1/other"
	if err := verifier().Verify(moved, body, prefix, pub); !errors.Is(err, ErrBadSignature) {
		t.Errorf("path: %v", err)
	}
	moved = good()
	moved.Method = http.MethodPut
	if err := verifier().Verify(moved, body, prefix, pub); !errors.Is(err, ErrBadSignature) {
		t.Errorf("method: %v", err)
	}
	// keyid is this key's prefix, and no other.
	if err := verifier().Verify(good(), body, "rst_ffffffffffff", pub); !errors.Is(err, ErrKeyID) {
		t.Errorf("keyid: %v", err)
	}
}

// Only the exact form is accepted: covered components and parameters are not a choice.
func TestOtherFormsAreRefused(t *testing.T) {
	pub, priv := newKey(t)
	body := []byte(`{}`)
	for name, change := range map[string]func(*http.Request){
		"fewer covered": func(r *http.Request) {
			r.Header.Set("Signature-Input", strings.Replace(r.Header.Get("Signature-Input"), ` "content-digest"`, "", 1))
		},
		"no nonce": func(r *http.Request) {
			r.Header.Set("Signature-Input", strings.Replace(r.Header.Get("Signature-Input"), `;nonce="`+nonce+`"`, "", 1))
		},
		"other tag": func(r *http.Request) {
			r.Header.Set("Signature-Input", strings.Replace(r.Header.Get("Signature-Input"), tag, "system-to-paradox", 1))
		},
		"other label": func(r *http.Request) {
			r.Header.Set("Signature-Input", strings.Replace(r.Header.Get("Signature-Input"), "rosetta=", "paradox=", 1))
		},
		"other algorithm": func(r *http.Request) {
			r.Header.Set("Signature-Input", strings.Replace(r.Header.Get("Signature-Input"), "ed25519", "rsa", 1))
		},
		"a parameter added": func(r *http.Request) { r.Header.Set("Signature-Input", r.Header.Get("Signature-Input")+";x=1") },
		"short signature":   func(r *http.Request) { r.Header.Set("Signature", "rosetta=:AAAA:") },
		"unframed":          func(r *http.Request) { r.Header.Set("Signature", "AAAA") },
	} {
		r := signed(body, "/api/v1/translate", priv, t0, nonce)
		change(r)
		if err := verifier().Verify(r, body, prefix, pub); !errors.Is(err, ErrMissing) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestANonceWorksOnce(t *testing.T) {
	pub, priv := newKey(t)
	body := []byte(`{}`)
	clock := t0
	v := Verifier{Nonces: &Nonces{}, Now: func() time.Time { return clock }}
	if err := v.Verify(signed(body, "/x", priv, t0, nonce), body, prefix, pub); err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(signed(body, "/x", priv, t0, nonce), body, prefix, pub); !errors.Is(err, ErrReplay) {
		t.Fatalf("again: %v", err)
	}
	// Another nonce is another request; the same nonce on another key is not this one.
	if err := v.Verify(signed(body, "/x", priv, t0, nonce+"-2"), body, prefix, pub); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(body))
	Sign(r, body, "rst_another00000", nonce, priv, t0)
	if err := v.Verify(r, body, "rst_another00000", pub); err != nil {
		t.Fatal(err)
	}
	// Past its window it is forgotten, so memory stays small; the signature is
	// refused for its age before the nonce matters.
	clock = t0.Add(Window + time.Second)
	if err := v.Verify(signed(body, "/x", priv, clock, nonce+"-3"), body, prefix, pub); err != nil {
		t.Fatal(err)
	}
	if v.Nonces.Size() != 1 {
		t.Fatalf("%d remembered", v.Nonces.Size())
	}
	if err := v.Verify(signed(body, "/x", priv, t0, nonce), body, prefix, pub); !errors.Is(err, ErrExpired) {
		t.Fatalf("%v", err)
	}
}

// A refused signature leaves no nonce behind, so nobody can fill the memory or use
// up a caller's nonce without its key.
func TestARefusedSignatureIsNotRemembered(t *testing.T) {
	pub, _ := newKey(t)
	_, other := newKey(t)
	body := []byte(`{}`)
	v := verifier()
	if err := v.Verify(signed(body, "/x", other, t0, nonce), body, prefix, pub); !errors.Is(err, ErrBadSignature) {
		t.Fatal(err)
	}
	if v.Nonces.Size() != 0 {
		t.Fatal("kept")
	}
}

// An error says what failed, never anything sent.
func TestErrorsHoldNothingSent(t *testing.T) {
	pub, priv := newKey(t)
	body := []byte(`CALLERTEXTMARKER`)
	r := signed(body, "/x", priv, t0, nonce)
	r.Header.Set("Signature-Input", "CALLERTEXTMARKER")
	err := verifier().Verify(r, body, prefix, pub)
	if err == nil || strings.Contains(err.Error(), "CALLERTEXTMARKER") {
		t.Fatal(err)
	}
}
