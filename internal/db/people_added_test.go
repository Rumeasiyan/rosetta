package db_test

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/dbtest"
)

// The migration must keep everyone with a live role, or the live administrator is locked out, and
// must not add someone who only ever signed in.
func TestTheMigrationKeepsLiveRoles(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	raw, err := os.ReadFile("migrations/012-people-added.sql")
	if err != nil {
		t.Fatal(err)
	}
	update := regexp.MustCompile(`(?s)UPDATE person p.*?;`).Find(raw)
	if update == nil {
		t.Fatal("no update")
	}
	for _, sql := range []string{
		`DELETE FROM role_grant WHERE person_id IN (SELECT id FROM person WHERE mjolnir_subject IN ('kept-role', 'kept-none'))`,
		`DELETE FROM person WHERE mjolnir_subject IN ('kept-role', 'kept-none')`,
		`INSERT INTO person (mjolnir_subject, name, email) VALUES ('kept-role', 'Invented', 'r@example.invalid'), ('kept-none', 'Invented', 'n@example.invalid')`,
		`INSERT INTO role_grant (person_id, role, post_ref) SELECT id, 'administrator', 'not known' FROM person WHERE mjolnir_subject = 'kept-role'`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, string(update)); err != nil {
		t.Fatal(err)
	}
	var role, none bool
	err = pool.QueryRow(ctx, `SELECT bool_or(added_at IS NOT NULL) FILTER (WHERE mjolnir_subject = 'kept-role'), bool_or(added_at IS NOT NULL) FILTER (WHERE mjolnir_subject = 'kept-none') FROM person`).Scan(&role, &none)
	if err != nil {
		t.Fatal(err)
	}
	if !role || none {
		t.Fatalf("role %v, none %v", role, none)
	}
	pool.Exec(ctx, `DELETE FROM role_grant WHERE person_id IN (SELECT id FROM person WHERE mjolnir_subject = 'kept-role')`)
	pool.Exec(ctx, `DELETE FROM person WHERE mjolnir_subject IN ('kept-role', 'kept-none')`)
}
