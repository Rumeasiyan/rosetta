// Package apisign checks the signature on a translation request from another machine
// (STACK section 5; E-CAL-02). It is PARADOX's fixed profile of HTTP Message
// Signatures (RFC 9421, Content-Digest as RFC 9530, docs/contract.md section 4 in
// PARADOX's repository) with one addition, a nonce, since a caller's request may be
// worth replaying and a job call is not:
//
//	Content-Digest: sha-256=:<base64>:
//	Signature-Input: rosetta=("@method" "@path" "content-digest");created=<unix>;keyid="<prefix>";nonce="<nonce>";alg="ed25519";tag="caller-to-rosetta"
//	Signature: rosetta=:<base64>:
//
// The signed text is the RFC's signature base for exactly these, joined by single
// newlines with none at the end. Only this form is accepted, so both sides' bases are
// the same byte for byte. @path leaves out the query, so a signed call carries none.
package apisign

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Window is how far a signature's time may be from ROSETTA's clock, either way.
const Window = 5 * time.Minute

const (
	label      = "rosetta"
	tag        = "caller-to-rosetta"
	components = `("@method" "@path" "content-digest")`
)

// Errors from Verify. Each means the same to a caller, "no key"; they differ only
// in the log, and none holds anything the caller sent.
var (
	ErrMissing      = errors.New("apisign: signature headers missing or malformed")
	ErrQuery        = errors.New("apisign: a signed call carries no query")
	ErrKeyID        = errors.New("apisign: keyid is not this key's prefix")
	ErrExpired      = errors.New("apisign: signature time outside the window")
	ErrDigest       = errors.New("apisign: body does not match Content-Digest")
	ErrBadSignature = errors.New("apisign: signature does not verify")
	ErrReplay       = errors.New("apisign: nonce seen already")
)

var nonceShape = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,128}$`)

// Digest is the Content-Digest of body.
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
}

func params(created int64, keyID, nonce string) string {
	return fmt.Sprintf(`%s;created=%d;keyid="%s";nonce="%s";alg="ed25519";tag="%s"`, components, created, keyID, nonce, tag)
}

func base(method, path, digest, params string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "\"@method\": %s\n", strings.ToUpper(method))
	fmt.Fprintf(&b, "\"@path\": %s\n", path)
	fmt.Fprintf(&b, "\"content-digest\": %s\n", digest)
	fmt.Fprintf(&b, "\"@signature-params\": %s", params)
	return b.Bytes()
}

func path(r *http.Request) string {
	if p := r.URL.EscapedPath(); p != "" {
		return p
	}
	return "/"
}

// Sign sets the three headers on r: what a caller does, and what the tests do.
// body must be exactly the bytes that will be sent.
func Sign(r *http.Request, body []byte, keyID, nonce string, key ed25519.PrivateKey, at time.Time) {
	digest := Digest(body)
	p := params(at.Unix(), keyID, nonce)
	sig := ed25519.Sign(key, base(r.Method, path(r), digest, p))
	r.Header.Set("Content-Digest", digest)
	r.Header.Set("Signature-Input", label+"="+p)
	r.Header.Set("Signature", label+"=:"+base64.StdEncoding.EncodeToString(sig)+":")
}

// Nonces remembers the nonces seen, in memory, for the window: a signature older
// than that is refused anyway. After a restart a request inside its window could be
// sent again once; the key and the rest of its signature still have to be right, and
// sending the same request again is safe (R-TRN-05). Memory stays small as only a
// verified signature is remembered.
type Nonces struct {
	mu   sync.Mutex
	seen map[string]time.Time // key prefix and nonce: until when
}

// use reports whether this nonce is new, and keeps it until until.
func (n *Nonces) use(keyID, nonce string, now, until time.Time) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.seen == nil {
		n.seen = map[string]time.Time{}
	}
	for k, t := range n.seen {
		if now.After(t) {
			delete(n.seen, k)
		}
	}
	k := keyID + " " + nonce
	if _, again := n.seen[k]; again {
		return false
	}
	n.seen[k] = until
	return true
}

// Size is how many are remembered (for tests).
func (n *Nonces) Size() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.seen)
}

// Verifier checks incoming requests.
type Verifier struct {
	Nonces *Nonces
	Now    func() time.Time // time.Now when nil
}

// Verify checks r against body, the whole body as received, for the key with this
// prefix and public key. Any error means the request must be refused.
func (v Verifier) Verify(r *http.Request, body []byte, prefix string, public ed25519.PublicKey) error {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return ErrQuery
	}
	created, keyID, nonce, err := parseInput(r.Header.Get("Signature-Input"))
	if err != nil {
		return err
	}
	sig, err := parseSignature(r.Header.Get("Signature"))
	if err != nil {
		return err
	}
	if keyID != prefix {
		return ErrKeyID
	}
	now := time.Now()
	if v.Now != nil {
		now = v.Now()
	}
	at := time.Unix(created, 0)
	if age := now.Sub(at); age > Window || age < -Window {
		return ErrExpired
	}
	digest := Digest(body)
	if r.Header.Get("Content-Digest") != digest {
		return ErrDigest
	}
	if len(public) != ed25519.PublicKeySize || !ed25519.Verify(public, base(r.Method, path(r), digest, params(created, keyID, nonce)), sig) {
		return ErrBadSignature
	}
	if !v.Nonces.use(keyID, nonce, now, at.Add(Window)) {
		return ErrReplay
	}
	return nil
}

// parseInput takes only the exact form this package writes.
func parseInput(h string) (created int64, keyID, nonce string, err error) {
	rest, ok := strings.CutPrefix(h, label+"="+components+";created=")
	if !ok {
		return 0, "", "", ErrMissing
	}
	num, rest, ok := strings.Cut(rest, `;keyid="`)
	if !ok {
		return 0, "", "", ErrMissing
	}
	if created, err = strconv.ParseInt(num, 10, 64); err != nil {
		return 0, "", "", ErrMissing
	}
	keyID, rest, ok = strings.Cut(rest, `";nonce="`)
	if !ok || keyID == "" || strings.ContainsAny(keyID, "\"\\") {
		return 0, "", "", ErrMissing
	}
	nonce, ok = strings.CutSuffix(rest, `";alg="ed25519";tag="`+tag+`"`)
	if !ok || !nonceShape.MatchString(nonce) {
		return 0, "", "", ErrMissing
	}
	if params(created, keyID, nonce) != strings.TrimPrefix(h, label+"=") {
		return 0, "", "", ErrMissing
	}
	return created, keyID, nonce, nil
}

func parseSignature(h string) ([]byte, error) {
	rest, ok := strings.CutPrefix(h, label+"=:")
	if !ok {
		return nil, ErrMissing
	}
	b64, ok := strings.CutSuffix(rest, ":")
	if !ok {
		return nil, ErrMissing
	}
	sig, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, ErrMissing
	}
	return sig, nil
}
