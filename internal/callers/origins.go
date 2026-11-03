package callers

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The browser addresses a system may call the grammar check from (R-CAL-06). A browser's
// Origin header is compared with them exactly, so only one way of writing each is kept.

// The refusals a change to the list can meet. None holds what was typed.
var (
	ErrBadOrigin    = errors.New("callers: not a browser address")
	ErrOriginTaken  = errors.New("callers: another system lists this address")
	ErrNoSuchOrigin = errors.New("callers: no system lists this address")
)

// uniqueViolation is PostgreSQL's code for a broken unique index.
const uniqueViolation = "23505"

// ParseOrigin reads a browser address as a browser sends it: https, or http only on this
// machine for local work, a host and perhaps a port, and nothing else. No wildcard can get
// through, since no host here holds one.
func ParseOrigin(text string) (string, error) {
	text = strings.TrimSpace(text)
	u, err := url.Parse(text)
	if err != nil || u.Host == "" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", ErrBadOrigin
	}
	scheme, host := strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	name := strings.ToLower(u.Hostname())
	switch scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(name); name != "localhost" && !strings.HasSuffix(name, ".localhost") && (ip == nil || !ip.IsLoopback()) {
			return "", ErrBadOrigin
		}
	default:
		return "", ErrBadOrigin
	}
	if strings.ContainsAny(host, "*?#%\\ ,") || name == "" {
		return "", ErrBadOrigin
	}
	return scheme + "://" + host, nil
}

// Origin is one listed address.
type Origin struct {
	Origin  string
	AddedAt time.Time
}

// Origins is the addresses one system lists.
func Origins(ctx context.Context, q Querier, callerID int64) ([]Origin, error) {
	rows, err := q.Query(ctx, `SELECT origin, added_at FROM caller_origin WHERE caller_id = $1 ORDER BY origin`, callerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Origin, error) {
		var o Origin
		err := r.Scan(&o.Origin, &o.AddedAt)
		return o, err
	})
}

// AddOrigin lists an address for a system, with its history, and says whether anything
// changed. An address another system lists is refused, as a call counts to one system.
func AddOrigin(ctx context.Context, tx pgx.Tx, s Caller, origin string, by int64) (bool, error) {
	var owner int64
	err := tx.QueryRow(ctx, `SELECT caller_id FROM caller_origin WHERE origin = $1`, origin).Scan(&owner)
	switch {
	case err == nil && owner == s.ID:
		return false, nil
	case err == nil:
		return false, ErrOriginTaken
	case !errors.Is(err, pgx.ErrNoRows):
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO caller_origin (caller_id, origin, added_by) VALUES ($1, $2, $3)`, s.ID, origin, by); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return false, ErrOriginTaken
		}
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_history (subject, subject_ref, new_value, changed_by) VALUES ('caller origin', $1, $2, $3)`, s.Reference, origin, by)
	return err == nil, err
}

// RemoveOrigin takes an address off a system's list, with its history, in force at once.
func RemoveOrigin(ctx context.Context, tx pgx.Tx, s Caller, origin string, by int64) (bool, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM caller_origin WHERE caller_id = $1 AND origin = $2`, s.ID, origin)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_history (subject, subject_ref, old_value, changed_by) VALUES ('caller origin', $1, $2, $3)`, s.Reference, origin, by)
	return err == nil, err
}

// FindOrigin is the system listing this address, read on every browser call, so a change
// is in force at once. The address is compared exactly.
func FindOrigin(ctx context.Context, q Querier, origin string) (Caller, error) {
	c, err := scan(q.QueryRow(ctx, `SELECT `+columns+` FROM caller WHERE id = (SELECT caller_id FROM caller_origin WHERE origin = $1)`, origin))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNoSuchOrigin
	}
	return c, err
}

// CallerUsed notes that a system was used now, on a call with no key (R-CAL-05).
func CallerUsed(ctx context.Context, q Querier, callerID int64) error {
	_, err := q.Exec(ctx, `UPDATE caller SET last_request_at = now() WHERE id = $1`, callerID)
	return err
}
