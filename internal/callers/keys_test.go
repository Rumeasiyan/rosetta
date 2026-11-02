package callers

import (
	"errors"
	"strings"
	"testing"

	"github.com/Rumeasiyan/paradox/sign"
	"github.com/Rumeasiyan/rosetta/internal/audit"
)

func TestNewKeyShowsOnlyItsPrefix(t *testing.T) {
	key, prefix, hash, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, prefix) || !strings.HasPrefix(prefix, "rst_") || len(prefix) != prefixLength || len(key) < prefixLength+40 {
		t.Fatal(prefix, len(key))
	}
	if hash != HashKey(key) || strings.Contains(hash, key) || len(hash) != 64 {
		t.Fatal(hash)
	}
	other, otherPrefix, otherHash, _ := NewKey()
	if other == key || otherPrefix == prefix || otherHash == hash {
		t.Fatal("two keys alike")
	}
}

// PENSIEVE refuses details that hold what looks like a key, so the prefix goes in
// object_ref, and even there it must not look like one to the audit check.
func TestThePrefixDoesNotLookLikeAKeyToTheAudit(t *testing.T) {
	_, prefix, _, _ := NewKey()
	if err := audit.CheckDetails(map[string]any{"shown": prefix}); err != nil {
		t.Fatal(err)
	}
}

func TestParsePublicKey(t *testing.T) {
	pub, priv, err := sign.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ParsePublicKey("  " + pub + "\n"); err != nil || got != pub {
		t.Fatal(got, err)
	}
	if got, err := ParsePublicKey("  "); err != nil || got != "" {
		t.Fatal("empty is a caller on this server")
	}
	for _, bad := range []string{priv, "CALLERTEXTMARKER", "ed25519-pub:c2hvcnQ="} {
		_, err := ParsePublicKey(bad)
		if !errors.Is(err, ErrBadPublicKey) || strings.Contains(err.Error(), bad) {
			t.Errorf("%v", err)
		}
	}
	k, err := PublicKey(pub)
	if err != nil || len(k) != 32 {
		t.Fatal(err)
	}
}

func TestKeyLive(t *testing.T) {
	if !(Key{}).Live() {
		t.Fatal("a key not turned off is live")
	}
}
