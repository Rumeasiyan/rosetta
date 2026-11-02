package callers

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/Rumeasiyan/paradox/sign"
	"github.com/jackc/pgx/v5"
)

// Keys follow STACK's one key rule (section 2; R-CAL-03): issued by the administrator
// or the system owner for their own system, shown once, two live at a time, turned
// off at once, each issue and turn-off audited.

// MaxLive is how many keys one system may hold live (STACK section 2; Annex A).
const MaxLive = 2

// keyStart begins every key, so one is recognised when it turns up where it should
// not be. prefixLength is how much of a key shows in lists.
const (
	keyStart     = "rst_"
	prefixLength = len(keyStart) + 12
)

// The refusals a change to a key can meet.
var (
	ErrTooManyKeys  = errors.New("callers: this system holds two live keys already")
	ErrNoSuchKey    = errors.New("callers: no such key")
	ErrBadPublicKey = errors.New("callers: not a public key in the signature form")
)

// NewKey makes a key: 256 random bits after the part that shows. The whole of it goes
// to the person once; only its hash and prefix are kept (R-NFR-10).
func NewKey() (key, prefix, hash string, err error) {
	shown := make([]byte, 6)
	secret := make([]byte, 32)
	if _, err = rand.Read(shown); err != nil {
		return "", "", "", err
	}
	if _, err = rand.Read(secret); err != nil {
		return "", "", "", err
	}
	prefix = keyStart + hex.EncodeToString(shown)
	key = prefix + base64.RawURLEncoding.EncodeToString(secret)
	return key, prefix, HashKey(key), nil
}

// HashKey is the one-way hash a key is stored and found by. A key has 256 random
// bits, so a fast hash is enough: there is nothing to guess.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// ParsePublicKey checks what was pasted when a key is issued: empty for a caller on
// this server, or one key in PARADOX's text form. It gives back the one way to write
// it, and a private key pasted by mistake is refused without being repeated.
func ParsePublicKey(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	k, err := sign.ParsePublicKey(text)
	if err != nil {
		return "", ErrBadPublicKey
	}
	return sign.FormatPublicKey(k), nil
}

// PublicKey reads a stored public key.
func PublicKey(text string) (ed25519.PublicKey, error) { return sign.ParsePublicKey(text) }

// Key is one key as lists show it: never the key.
type Key struct {
	ID          int64
	CallerID    int64
	Prefix      string
	Signs       bool // holds a public key, so its requests must be signed
	CreatedAt   time.Time
	CreatedBy   string
	LastUsedAt  *time.Time
	TurnedOffAt *time.Time
}

// Live says the key still works.
func (k Key) Live() bool { return k.TurnedOffAt == nil }

// Issue makes a key for a system, refusing a third live one. It holds the system's
// row, so two people issuing at once cannot make three. It returns the key itself,
// to be shown once.
func Issue(ctx context.Context, tx pgx.Tx, callerID, by int64, publicKey string) (string, Key, error) {
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM caller WHERE id = $1 FOR UPDATE`, callerID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", Key{}, ErrNoSuchSystem
		}
		return "", Key{}, err
	}
	var live int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM caller_key WHERE caller_id = $1 AND turned_off_at IS NULL`, callerID).Scan(&live); err != nil {
		return "", Key{}, err
	}
	if live >= MaxLive {
		return "", Key{}, ErrTooManyKeys
	}
	key, prefix, hash, err := NewKey()
	if err != nil {
		return "", Key{}, err
	}
	k := Key{CallerID: callerID, Prefix: prefix, Signs: publicKey != ""}
	err = tx.QueryRow(ctx, `INSERT INTO caller_key (caller_id, key_hash, prefix, public_key, created_by)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5) RETURNING id, created_at`, callerID, hash, prefix, publicKey, by).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return "", Key{}, err
	}
	return key, k, nil
}

// TurnOff ends a live key of this system at once, which revokes its public key too.
func TurnOff(ctx context.Context, tx pgx.Tx, callerID, keyID, by int64) (Key, error) {
	k := Key{ID: keyID, CallerID: callerID}
	err := tx.QueryRow(ctx, `UPDATE caller_key SET turned_off_at = now(), turned_off_by = $3
		WHERE id = $1 AND caller_id = $2 AND turned_off_at IS NULL RETURNING prefix, public_key IS NOT NULL, created_at, turned_off_at`,
		keyID, callerID, by).Scan(&k.Prefix, &k.Signs, &k.CreatedAt, &k.TurnedOffAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNoSuchKey
	}
	return k, err
}

// keyHistory is the most keys a system's screen shows.
const keyHistory = 50

// Keys is a system's keys, live first, then the latest turned off.
func Keys(ctx context.Context, q Querier, callerID int64) ([]Key, error) {
	rows, err := q.Query(ctx, `SELECT k.id, k.caller_id, k.prefix, k.public_key IS NOT NULL, k.created_at, COALESCE(p.name, ''), k.last_used_at, k.turned_off_at
		FROM caller_key k LEFT JOIN person p ON p.id = k.created_by
		WHERE k.caller_id = $1 ORDER BY k.turned_off_at IS NOT NULL, k.turned_off_at DESC, k.id DESC LIMIT $2`, callerID, keyHistory)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Key, error) {
		var k Key
		err := r.Scan(&k.ID, &k.CallerID, &k.Prefix, &k.Signs, &k.CreatedAt, &k.CreatedBy, &k.LastUsedAt, &k.TurnedOffAt)
		return k, err
	})
}

// Holder is what a key belongs to, as a request needs it.
type Holder struct {
	KeyID     int64
	Prefix    string
	PublicKey string // "" for a caller on this server
	TurnedOff bool
	Caller    Caller
}

// FindKey is the key with this hash, read on every request, so turning one off takes
// effect at once.
func FindKey(ctx context.Context, q Querier, hash string) (Holder, error) {
	var h Holder
	var c Caller
	err := q.QueryRow(ctx, `SELECT k.id, k.prefix, COALESCE(k.public_key, ''), k.turned_off_at IS NOT NULL,
		c.id, c.tva_reference, c.name, COALESCE(c.owner_post_ref, ''), COALESCE(c.owner_subject, ''), c.tva_status, c.tva_checked_at,
		c.requests_per_minute, c.requests_at_once, c.registered_at, c.last_request_at, c.flag
		FROM caller_key k JOIN caller c ON c.id = k.caller_id WHERE k.key_hash = $1`, hash).Scan(
		&h.KeyID, &h.Prefix, &h.PublicKey, &h.TurnedOff,
		&c.ID, &c.Reference, &c.Name, &c.OwnerPostRef, &c.OwnerSubject, &c.TVAStatus, &c.TVACheckedAt,
		&c.PerMinute, &c.AtOnce, &c.RegisteredAt, &c.LastRequestAt, &c.Flag)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, ErrNoSuchKey
	}
	h.Caller = c
	return h, err
}

// Used notes that a key and its system were used now (Annex A; R-CAL-05).
func Used(ctx context.Context, q Querier, keyID, callerID int64) error {
	if _, err := q.Exec(ctx, `UPDATE caller_key SET last_used_at = now() WHERE id = $1`, keyID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `UPDATE caller SET last_request_at = now() WHERE id = $1`, callerID)
	return err
}
