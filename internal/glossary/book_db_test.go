package glossary_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Empty empties the glossary for a test. In the service nothing is ever deleted.
func empty(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, sql := range []string{`DELETE FROM alert`, `DELETE FROM cache_piece`, `DELETE FROM memory_entry`, `DELETE FROM glossary_term`, `DELETE FROM glossary_entry`, `DELETE FROM glossary_import`, `DELETE FROM glossary_version WHERE number > 0`} {
		if _, err := pool.Exec(context.Background(), sql); err != nil {
			t.Fatal(err)
		}
	}
}

func keeper(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	subject := fmt.Sprintf("book-test-%d", time.Now().UnixNano())
	if err := pool.QueryRow(context.Background(), `INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, 'Invented', 'b@example.invalid') RETURNING id`, subject).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// publish adds an entry and checks all three languages.
func publish(t *testing.T, pool *pgxpool.Pool, by int64, kind string, en, si, ta string, replaces *int64) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var err error
		var langs []i18n.Lang
		id, langs, err = glossary.Propose(ctx, tx, glossary.Proposal{Type: kind, By: by, Replaces: replaces, Texts: map[i18n.Lang]string{i18n.EN: en, i18n.SI: si, i18n.TA: ta}})
		if err != nil {
			return err
		}
		for _, l := range langs {
			if _, err := glossary.Check(ctx, tx, id, l, "", by); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func found(t *testing.T, b *glossary.Book, from, to, text string) (int, []string) {
	t.Helper()
	v, set, err := b.Terms(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range set.Find(text) {
		out = append(out, f.Term.Target)
	}
	return v, out
}

// A published change is in force on the next request, in each direction, and what waits is not.
func TestTheBookFollowsTheVersion(t *testing.T) {
	pool := dbtest.Open(t)
	empty(t, pool)
	by := keeper(t, pool)
	b := &glossary.Book{Q: pool}
	if v, got := found(t, b, "en", "ta", "The Chief Secretary"); v != 0 || len(got) != 0 {
		t.Fatalf("%d %v", v, got)
	}
	id := publish(t, pool, by, glossary.TypePost, "Chief Secretary", "ප්‍රධාන ලේකම්", "பிரதம செயலாளர்", nil)
	if v, got := found(t, b, "en", "ta", "The Chief Secretary"); v != 1 || strings.Join(got, "") != "பிரதம செயலாளர்" {
		t.Fatalf("%d %v", v, got)
	}
	if v, got := found(t, b, "ta", "si", "இன்று பிரதம செயலாளர் வந்தார்"); v != 1 || strings.Join(got, "") != "ප්‍රධාන ලේකම්" {
		t.Fatalf("%d %v", v, got)
	}
	if _, got := found(t, b, "en", "en", "The Chief Secretary"); strings.Join(got, "") != "Chief Secretary" {
		t.Fatal(got)
	}
	// A proposal is not in force.
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, _, err := glossary.Propose(ctx, tx, glossary.Proposal{Type: glossary.TypeTerm, By: by, Texts: map[i18n.Lang]string{i18n.EN: "Waiting Term"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, got := found(t, b, "en", "ta", "A Waiting Term"); len(got) != 0 {
		t.Fatal(got)
	}
	// A correction replaces it at once, and the version moves by one.
	publish(t, pool, by, glossary.TypePost, "Chief Secretary", "", "தலைமைச் செயலாளர்", &id)
	if v, got := found(t, b, "en", "ta", "The Chief Secretary"); v != 2 || strings.Join(got, "") != "தலைமைச் செயலாளர்" {
		t.Fatalf("%d %v", v, got)
	}
	// Keep as written is the same in every direction.
	publish(t, pool, by, glossary.TypeKeep, "SCOS Pro", "", "", nil)
	if v, got := found(t, b, "si", "ta", "SCOS Pro"); v != 3 || strings.Join(got, "") != "SCOS Pro" {
		t.Fatalf("%d %v", v, got)
	}
}
