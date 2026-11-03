package db_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/dbtest"
)

// Every entity and field in the PRD's data dictionary (Annex A.2) is a table
// and a column, so the schema and the document cannot drift apart.
func TestSchemaMatchesTheDictionary(t *testing.T) {
	pool := dbtest.Open(t)
	raw, err := os.ReadFile("../../docs/ROSETTA-PRD.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	start, end := strings.Index(doc, "<!-- dictionary:start -->"), strings.Index(doc, "<!-- dictionary:end -->")
	if start < 0 || end < start {
		t.Fatal("no dictionary in the PRD")
	}
	// Notes in the dictionary, not columns: "kept" is a retention rule, and
	// "pieces by origin" is the four pieces_* columns.
	notes := map[string]bool{"kept": true, "pieces by origin": true}
	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\| (.*) \\|$")
	field := regexp.MustCompile("(?:^|; )`([a-z_, ]+)`:")
	rows := row.FindAllStringSubmatch(doc[start:end], -1)
	if len(rows) != 22 {
		t.Fatalf("%d entities in the dictionary, want 22", len(rows))
	}
	ctx := context.Background()
	for _, r := range rows {
		table := r[1]
		for _, f := range field.FindAllStringSubmatch(r[2], -1) {
			if notes[f[1]] {
				continue
			}
			for _, col := range strings.Split(f[1], ", ") {
				var found bool
				err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2)`, table, col).Scan(&found)
				if err != nil {
					t.Fatal(err)
				}
				if !found {
					t.Errorf("%s.%s is in the dictionary but not the schema", table, col)
				}
			}
		}
	}
}

func TestTextSearchCacheAndPartitions(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	var persistence string
	if err := pool.QueryRow(ctx, `SELECT relpersistence::text FROM pg_class WHERE relname = 'cache_piece'`).Scan(&persistence); err != nil {
		t.Fatal(err)
	}
	if persistence != "u" {
		t.Errorf("cache_piece persistence %q, want UNLOGGED", persistence)
	}
	var parts int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_inherits WHERE inhparent = 'usage_record'::regclass`).Scan(&parts); err != nil {
		t.Fatal(err)
	}
	if parts < 2 {
		t.Errorf("%d usage parts, want this month and the next", parts)
	}
	var trgm bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`).Scan(&trgm); err != nil {
		t.Fatal(err)
	}
	if !trgm {
		t.Error("pg_trgm is missing")
	}
}

func TestRowRules(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	for _, sql := range []string{
		// System owner comes from TVA, never a grant (R-ADM-06).
		`INSERT INTO person (mjolnir_subject, name, email) VALUES ('rule-x', 'Invented', 'x@example.invalid');
		 INSERT INTO role_grant (person_id, role) SELECT id, 'system owner' FROM person WHERE mjolnir_subject = 'rule-x'`,
		// A keeper has a language; an administrator has none.
		`INSERT INTO person (mjolnir_subject, name, email) VALUES ('rule-y', 'Invented', 'y@example.invalid');
		 INSERT INTO role_grant (person_id, role) SELECT id, 'glossary keeper' FROM person WHERE mjolnir_subject = 'rule-y'`,
		// A direction opens only on a recorded evaluation (R-GAT-01).
		`INSERT INTO person (mjolnir_subject, name, email) VALUES ('rule-z', 'Invented', 'z@example.invalid');
		 INSERT INTO source (name, version, state, seen_at) VALUES ('rule-source', '1', 'on', now());
		 INSERT INTO gate_decision (source_id, from_language, to_language, source_version, state, decided_by)
		 SELECT s.id, 'en', 'ta', '1', 'open', p.id FROM source s, person p WHERE s.name = 'rule-source' AND p.mjolnir_subject = 'rule-z'`,
		// Other languages are refused.
		`INSERT INTO person (mjolnir_subject, name, email, screen_language) VALUES ('rule-w', 'Invented', 'w@example.invalid', 'fr')`,
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, execErr := tx.Exec(ctx, sql)
		tx.Rollback(ctx)
		if execErr == nil {
			t.Errorf("accepted: %s", sql)
		}
	}
}
