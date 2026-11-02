// Package people keeps the person's record (Annex A: person): who MJOLNIR says
// they are at the last sign-in, their screen language and when they were last
// seen. There is no second sign-in history (P-ING-02): MJOLNIR records sign-ins.
package people

import (
	"context"
	"errors"

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
