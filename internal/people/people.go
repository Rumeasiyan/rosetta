// Package people keeps the person's record (Annex A: person): who MJOLNIR says
// they are at the last sign-in, their screen language and when they were last
// seen. There is no second sign-in history (P-ING-02): MJOLNIR records sign-ins.
// It also keeps the roles they hold (Annex A: role_grant; R-ADM-06).
package people

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Person is one person's record.
type Person struct {
	ID       int64
	Subject  string
	Name     string
	Email    string
	Language string
	IsNew    bool // first time seen
}

// Querier is what these run through: a pool or a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Upsert keeps the person as MJOLNIR sent them, matched on the subject alone.
func Upsert(ctx context.Context, q Querier, subject, name, email string) (Person, error) {
	p := Person{Subject: subject, Name: name, Email: email}
	err := q.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, $2, $3)
		ON CONFLICT (mjolnir_subject) DO UPDATE SET name = EXCLUDED.name, email = EXCLUDED.email, last_seen_at = now()
		RETURNING id, screen_language, first_seen_at = last_seen_at`, subject, name, email).Scan(&p.ID, &p.Language, &p.IsNew)
	return p, err
}

// Seen marks the person as seen now, at a renewal on their own request.
func Seen(ctx context.Context, q Querier, id int64) error {
	_, err := q.Exec(ctx, "UPDATE person SET last_seen_at = now() WHERE id = $1", id)
	return err
}

// SetLanguage remembers the person's screen language (R-NFR-05).
func SetLanguage(ctx context.Context, q Querier, id int64, language string) error {
	switch language {
	case "si", "ta", "en":
	default:
		return errors.New("people: not a screen language")
	}
	_, err := q.Exec(ctx, "UPDATE person SET screen_language = $2 WHERE id = $1", id, language)
	return err
}

// The roles ROSETTA grants (R-ADM-06). System owner is never one: it comes from
// TVA's file (R-CAL-02).
const (
	Administrator  = "administrator"
	GlossaryKeeper = "glossary keeper"
	MemoryReviewer = "memory reviewer"
)

// PostNotKnown is post_ref until a post look-up finds the person.
const PostNotKnown = "not known"

// liveSQL is what makes a grant live; its end date counts through its whole day in
// Sri Lanka, and ending one takes effect at once.
const liveSQL = `g.ended_at IS NULL AND g.starts_at <= now()
	AND (g.ends_on IS NULL OR g.ends_on >= (now() AT TIME ZONE 'Asia/Colombo')::date)`

// lockRoles keeps two changes to who is an administrator from crossing.
const lockRoles = 0x52535401

// The refusals a change to a role can meet.
var (
	ErrBadRole           = errors.New("people: not a role that can be granted")
	ErrBadLanguage       = errors.New("people: this role needs one language, and only this kind does")
	ErrBadDate           = errors.New("people: the end date is not a date from today on")
	ErrNoSuchPerson      = errors.New("people: no such person")
	ErrAlreadyHeld       = errors.New("people: the person holds this role already")
	ErrNoSuchGrant       = errors.New("people: no such live role")
	ErrLastAdministrator = errors.New("people: the last administrator cannot be ended")
)

// Grant is one role held.
type Grant struct {
	ID       int64
	PersonID int64
	Subject  string
	Name     string
	Email    string
	Role     string
	Language i18n.Lang // empty for an administrator
	PostRef  string
	// PostFlagged says BIFROST answered and the person's post is still not known (R-ADM-06).
	PostFlagged bool
	StartsAt    time.Time
	EndsOn      *time.Time
	EndedAt     *time.Time
}

// Request is a grant as asked for, checked by ParseRequest.
type Request struct {
	Role     string
	Language *string
	EndsOn   *time.Time
}

// ParseRequest checks what the form sent. today is the date in Sri Lanka.
func ParseRequest(role, language, endsOn string, today time.Time) (Request, error) {
	var out Request
	switch role {
	case Administrator:
		if language != "" {
			return out, ErrBadLanguage
		}
	case GlossaryKeeper, MemoryReviewer:
		l, ok := i18n.Parse(language)
		if !ok {
			return out, ErrBadLanguage
		}
		s := string(l)
		out.Language = &s
	default:
		return out, ErrBadRole
	}
	out.Role = role
	if endsOn != "" {
		d, err := time.Parse(time.DateOnly, endsOn)
		if err != nil || d.Before(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
			return out, ErrBadDate
		}
		out.EndsOn = &d
	}
	return out, nil
}

const grantColumns = `g.id, g.person_id, p.mjolnir_subject, p.name, p.email, g.role, COALESCE(g.language, ''), g.post_ref, g.post_flagged_at IS NOT NULL, g.starts_at, g.ends_on, g.ended_at`

func scanGrant(r pgx.CollectableRow) (Grant, error) {
	var g Grant
	var lang string
	err := r.Scan(&g.ID, &g.PersonID, &g.Subject, &g.Name, &g.Email, &g.Role, &lang, &g.PostRef, &g.PostFlagged, &g.StartsAt, &g.EndsOn, &g.EndedAt)
	g.Language = i18n.Lang(lang)
	return g, err
}

// Live is one person's live roles, read on every request so that a role ended
// takes effect at once.
func Live(ctx context.Context, q Querier, personID int64) ([]Grant, error) {
	rows, err := q.Query(ctx, `SELECT `+grantColumns+` FROM role_grant g JOIN person p ON p.id = g.person_id
		WHERE g.person_id = $1 AND `+liveSQL+` ORDER BY g.id`, personID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanGrant)
}

// AllLive is everyone's live roles, for the administrator's screen.
func AllLive(ctx context.Context, q Querier) ([]Grant, error) {
	rows, err := q.Query(ctx, `SELECT `+grantColumns+` FROM role_grant g JOIN person p ON p.id = g.person_id
		WHERE `+liveSQL+` ORDER BY p.name, g.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanGrant)
}

// Today is the date in Sri Lanka, as q's clock has it.
func Today(ctx context.Context, q Querier) (time.Time, error) {
	var d time.Time
	err := q.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'Asia/Colombo')::date`).Scan(&d)
	return d, err
}

// FirstAdministrator makes person the first administrator, only when bootstrap
// names them and no administrator exists. It reports whether it did. bootstrap is
// their email at MJOLNIR or their account ID: nothing shows the account ID before a
// first sign-in, so the Office can only be sure of the email.
func FirstAdministrator(ctx context.Context, tx pgx.Tx, person Person, bootstrap string) (bool, error) {
	if bootstrap == "" || (person.Subject != bootstrap && !strings.EqualFold(person.Email, bootstrap)) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(lockRoles)); err != nil {
		return false, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role_grant g WHERE g.role = $1 AND `+liveSQL+`)`, Administrator).Scan(&exists); err != nil || exists {
		return false, err
	}
	_, err := tx.Exec(ctx, `INSERT INTO role_grant (person_id, role, post_ref) VALUES ($1, $2, $3)`, person.ID, Administrator, PostNotKnown)
	return err == nil, err
}

// GrantRole gives a role to the person who signed in last with this email, and
// returns them. The role is held by their account: the email only finds it among
// those who have signed in, since a list of them could outgrow a page (R-NFR-03).
func GrantRole(ctx context.Context, tx pgx.Tx, email string, by int64, req Request) (Person, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(lockRoles)); err != nil {
		return Person{}, err
	}
	var p Person
	err := tx.QueryRow(ctx, `SELECT id, mjolnir_subject, name, email FROM person WHERE lower(email) = lower($1) AND email <> ''
		ORDER BY last_seen_at DESC LIMIT 1`, strings.TrimSpace(email)).Scan(&p.ID, &p.Subject, &p.Name, &p.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNoSuchPerson
	}
	if err != nil {
		return p, err
	}
	var held bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role_grant g WHERE g.person_id = $1 AND g.role = $2
		AND g.language IS NOT DISTINCT FROM $3::text AND `+liveSQL+`)`, p.ID, req.Role, req.Language).Scan(&held); err != nil {
		return p, err
	}
	if held {
		return p, ErrAlreadyHeld
	}
	_, err = tx.Exec(ctx, `INSERT INTO role_grant (person_id, role, language, post_ref, ends_on, granted_by)
		VALUES ($1, $2, $3, $4, $5, $6)`, p.ID, req.Role, req.Language, PostNotKnown, req.EndsOn, by)
	return p, err
}

// EndRole ends a live role at once (R-ADM-06), and returns it. The last
// administrator stays: nobody could then grant anything.
func EndRole(ctx context.Context, tx pgx.Tx, grantID, by int64) (Grant, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(lockRoles)); err != nil {
		return Grant{}, err
	}
	rows, err := tx.Query(ctx, `SELECT `+grantColumns+` FROM role_grant g JOIN person p ON p.id = g.person_id
		WHERE g.id = $1 AND `+liveSQL, grantID)
	if err != nil {
		return Grant{}, err
	}
	g, err := pgx.CollectExactlyOneRow(rows, scanGrant)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNoSuchGrant
	}
	if err != nil {
		return g, err
	}
	if g.Role == Administrator {
		var others bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role_grant g WHERE g.role = $1 AND g.id <> $2 AND `+liveSQL+`)`, Administrator, grantID).Scan(&others); err != nil {
			return g, err
		}
		if !others {
			return g, ErrLastAdministrator
		}
	}
	_, err = tx.Exec(ctx, `UPDATE role_grant SET ended_at = now(), ended_by = $2 WHERE id = $1`, grantID, by)
	return g, err
}

// LiveForUpdate is one person's live roles, held for a look-up's transaction.
func LiveForUpdate(ctx context.Context, tx pgx.Tx, personID int64) ([]Grant, error) {
	rows, err := tx.Query(ctx, `SELECT `+grantColumns+` FROM role_grant g JOIN person p ON p.id = g.person_id
		WHERE g.person_id = $1 AND `+liveSQL+` ORDER BY g.id FOR UPDATE OF g`, personID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanGrant)
}

// SetPost keeps what a look-up found: the post, or that the person was answered as not found, or
// only that BIFROST answered. A failed look-up never comes here.
func SetPost(ctx context.Context, tx pgx.Tx, grantID int64, post string, flagged bool) error {
	_, err := tx.Exec(ctx, `UPDATE role_grant SET post_ref = $2, post_checked_at = now(),
		post_flagged_at = CASE WHEN $3 THEN COALESCE(post_flagged_at, now()) END WHERE id = $1`, grantID, post, flagged)
	return err
}

// EndForPost ends a role at once because the person no longer holds its post (R-ADM-06). ROSETTA
// ended it, so ended_by stays empty. Unlike a person's own click this may end the last administrator:
// the bootstrap setting then works again, and R-ADM-06 makes no exception.
func EndForPost(ctx context.Context, tx pgx.Tx, grantID int64) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", int64(lockRoles)); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE role_grant SET ended_at = now(), post_checked_at = now() WHERE id = $1`, grantID)
	return err
}

// EndPastDate ends each role whose end date has passed in Sri Lanka, which has already stopped
// being live, and returns them for their audit entries (E-CAL-05).
func EndPastDate(ctx context.Context, tx pgx.Tx) ([]Grant, error) {
	rows, err := tx.Query(ctx, `WITH ended AS (UPDATE role_grant g SET ended_at = now()
			WHERE g.ended_at IS NULL AND g.ends_on < (now() AT TIME ZONE 'Asia/Colombo')::date RETURNING g.*)
		SELECT `+grantColumns+` FROM ended g JOIN person p ON p.id = g.person_id ORDER BY g.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanGrant)
}

// PersonToLookUp is one person holding a live role.
type PersonToLookUp struct {
	ID      int64
	Subject string
	Email   string
}

// HoldingRoles is everyone holding a live role, each once, for the daily pass.
func HoldingRoles(ctx context.Context, q Querier) ([]PersonToLookUp, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT p.id, p.mjolnir_subject, p.email FROM role_grant g JOIN person p ON p.id = g.person_id WHERE `+liveSQL+` ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PersonToLookUp, error) {
		var x PersonToLookUp
		err := r.Scan(&x.ID, &x.Subject, &x.Email)
		return x, err
	})
}

// FlaggedPeople is who is flagged now: their post is still not known after BIFROST answered.
func FlaggedPeople(ctx context.Context, q Querier) ([]PersonToLookUp, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT p.id, p.mjolnir_subject, p.email FROM role_grant g JOIN person p ON p.id = g.person_id
		WHERE g.post_flagged_at IS NOT NULL AND `+liveSQL+` ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PersonToLookUp, error) {
		var x PersonToLookUp
		err := r.Scan(&x.ID, &x.Subject, &x.Email)
		return x, err
	})
}

// LanguagesWithoutKeeper is each language no live glossary keeper holds (s2).
func LanguagesWithoutKeeper(ctx context.Context, q Querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT l FROM unnest(ARRAY['si', 'ta', 'en']) AS l WHERE NOT EXISTS (
		SELECT 1 FROM role_grant g WHERE g.role = $1 AND g.language = l AND `+liveSQL+`)`, GlossaryKeeper)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// OwnedSystem is a calling system TVA's file says this person answers for.
type OwnedSystem struct {
	Reference string
	Name      string
}

// Owned is the systems whose owner is subject: the system owner role comes from
// TVA's file, matched on the subject and never on email (R-CAL-02).
func Owned(ctx context.Context, q Querier, subject string) ([]OwnedSystem, error) {
	rows, err := q.Query(ctx, `SELECT tva_reference, name FROM caller WHERE owner_subject = $1 ORDER BY name`, subject)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (OwnedSystem, error) {
		var s OwnedSystem
		err := r.Scan(&s.Reference, &s.Name)
		return s, err
	})
}
