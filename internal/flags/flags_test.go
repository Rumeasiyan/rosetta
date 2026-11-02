package flags_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/flags"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/tva"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fileReader struct {
	file tva.File
	err  error
}

func (f fileReader) Read(context.Context) (tva.File, error) { return f.file, f.err }

func ptr(s string) *string { return &s }

func entry(ref, status, subject, name string) tva.System {
	s := tva.System{Reference: ref, Status: status, NameEN: ptr(name), OwnerPostRef: ptr("31")}
	if subject != "" {
		s.OwnerSubject = ptr(subject)
	}
	return s
}

func clean(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, sql := range []string{`DELETE FROM usage_record`, `DELETE FROM daily_total`, `DELETE FROM memory_entry`, `DELETE FROM alert`, `DELETE FROM caller_origin`, `DELETE FROM caller_key`, `DELETE FROM caller`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
}

func run(t *testing.T, pool *pgxpool.Pool, d *flags.Daily) {
	t.Helper()
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { _, err := d.Run(ctx, tx, "run"); return err }); err != nil {
		t.Fatal(err)
	}
}

func system(t *testing.T, pool *pgxpool.Pool, ref, status, subject string, registered time.Time) callers.Caller {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at, registered_at) VALUES ($1, 'Old name', NULLIF($2, ''), $3, now(), $4) RETURNING id`, ref, subject, status, registered).Scan(&id); err != nil {
		t.Fatal(err)
	}
	c, err := callers.Get(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func read(t *testing.T, pool *pgxpool.Pool, id int64) callers.Caller {
	t.Helper()
	c, err := callers.Get(context.Background(), pool, id)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRefreshAndFlags(t *testing.T) {
	pool := dbtest.Open(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	now := time.Now()
	fresh := system(t, pool, "FLAG-1", "live", "owner-1", now)
	quiet := system(t, pool, "FLAG-2", "live", "owner-1", now.AddDate(0, 0, -91))
	used := system(t, pool, "FLAG-3", "live", "owner-1", now.AddDate(0, 0, -91))
	vacant := system(t, pool, "FLAG-4", "live", "owner-1", now)
	retired := system(t, pool, "FLAG-5", "live", "owner-1", now)
	gone := system(t, pool, "FLAG-6", "live", "owner-1", now)
	if _, err := pool.Exec(context.Background(), `UPDATE caller SET last_request_at = now() WHERE id = $1`, used.ID); err != nil {
		t.Fatal(err)
	}
	file := tva.File{Systems: []tva.System{
		entry("FLAG-1", "may_join", "owner-new", "New name"), entry("FLAG-2", "live", "owner-1", "Quiet"), entry("FLAG-3", "live", "owner-1", "Used"),
		entry("FLAG-4", "live", "", "Vacant"), entry("FLAG-5", "retired", "owner-1", "Retired"),
	}}
	var told flags.Changes
	d := &flags.Daily{TVA: fileReader{file: file}, Settings: func(context.Context) settings.Values { return settings.Defaults() },
		Told: func(_ context.Context, _ pgx.Tx, c flags.Changes) error { told = c; return nil }}
	run(t, pool, d)

	if c := read(t, pool, fresh.ID); c.Name != "New name" || c.OwnerSubject != "owner-new" || c.TVAStatus != "may_join" || c.Flag != callers.FlagNone || c.OwnerPostRef != "31" {
		t.Fatalf("%+v", c)
	}
	if c := read(t, pool, quiet.ID); c.Flag != callers.FlagIdle {
		t.Fatalf("%+v", c)
	}
	if c := read(t, pool, used.ID); c.Flag != callers.FlagNone {
		t.Fatalf("a request counts: %+v", c)
	}
	if c := read(t, pool, vacant.ID); c.Flag != callers.FlagNoOwner || c.OwnerSubject != "" {
		t.Fatalf("%+v", c)
	}
	if c := read(t, pool, retired.ID); c.Flag != callers.FlagNotLive || c.TVAStatus != "retired" {
		t.Fatalf("%+v", c)
	}
	// One gone from the file is flagged and keeps what was last read: the administrator decides.
	if c := read(t, pool, gone.ID); c.Flag != callers.FlagNotLive || c.TVAStatus != "live" {
		t.Fatalf("%+v", c)
	}
	if len(told.Systems) != 4 {
		t.Fatalf("%+v", told)
	}
	// Run again: nothing moves, so nobody would be told twice.
	run(t, pool, d)
	if len(told.Systems) != 0 {
		t.Fatalf("%+v", told)
	}
	// The setting moves the idle line.
	d.Settings = func(context.Context) settings.Values {
		v := settings.Defaults()
		v[settings.CallerIdleDays.Name] = 365
		return v
	}
	run(t, pool, d)
	if c := read(t, pool, quiet.ID); c.Flag != callers.FlagNone {
		t.Fatalf("%+v", c)
	}
}

// A file that cannot be read, or holds nothing, changes no copy (a damaged file is not every system gone).
func TestAnUnreadFileChangesNoCopy(t *testing.T) {
	pool := dbtest.Open(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	s := system(t, pool, "FLAG-7", "live", "owner-1", time.Now())
	values := func(context.Context) settings.Values { return settings.Defaults() }
	for _, r := range []fileReader{{err: errors.New("no file")}, {file: tva.File{}}} {
		run(t, pool, &flags.Daily{TVA: r, Settings: values})
		if c := read(t, pool, s.ID); c.Flag != callers.FlagNone || c.Name != "Old name" || c.TVAStatus != "live" {
			t.Fatalf("%+v", c)
		}
	}
}

// A role past its end date ends with an audit entry; one reaching today stays, as it counts through
// its whole day (the decision on roles).
func TestRolesPastTheirDateEndWithAnEntry(t *testing.T) {
	pool := dbtest.Open(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	ctx := context.Background()
	subject := "flag-person-" + time.Now().Format("150405.000000")
	var person int64
	if err := pool.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, 'Invented', 'flag@example.invalid') RETURNING id`, subject).Scan(&person); err != nil {
		t.Fatal(err)
	}
	var past, today int64
	if err := pool.QueryRow(ctx, `INSERT INTO role_grant (person_id, role, language, ends_on) VALUES ($1, 'glossary keeper', 'ta', (now() AT TIME ZONE 'Asia/Colombo')::date - 1) RETURNING id`, person).Scan(&past); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO role_grant (person_id, role, language, ends_on) VALUES ($1, 'memory reviewer', 'ta', (now() AT TIME ZONE 'Asia/Colombo')::date) RETURNING id`, person).Scan(&today); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `UPDATE role_grant SET ended_at = now() WHERE person_id = $1`, person)
	})
	values := func(context.Context) settings.Values { return settings.Defaults() }
	run(t, pool, &flags.Daily{Settings: values})
	var ended bool
	if err := pool.QueryRow(ctx, `SELECT ended_at IS NOT NULL FROM role_grant WHERE id = $1`, past).Scan(&ended); err != nil || !ended {
		t.Fatal(ended, err)
	}
	if err := pool.QueryRow(ctx, `SELECT ended_at IS NOT NULL FROM role_grant WHERE id = $1`, today).Scan(&ended); err != nil || ended {
		t.Fatal(ended, err)
	}
	var details string
	if err := pool.QueryRow(ctx, `SELECT details::text FROM audit_outbox WHERE details->>'object_ref' = $1`, subject).Scan(&details); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(details, "end date has passed") || !strings.Contains(details, `"post": "system"`) {
		t.Fatal(details)
	}
	// Run again: it is ended, and writes nothing more.
	run(t, pool, &flags.Daily{Settings: values})
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_outbox WHERE details->>'object_ref' = $1`, subject).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

// A language with no live keeper is flagged (s2), and stops being when one is named.
func TestALanguageWithNoKeeper(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE role_grant SET ended_at = now() WHERE role = 'glossary keeper' AND ended_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	got, err := people.LanguagesWithoutKeeper(ctx, pool)
	if err != nil || len(got) != 3 {
		t.Fatal(got, err)
	}
	var person int64
	if err := pool.QueryRow(ctx, `INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, 'Invented', 'lang@example.invalid') RETURNING id`, "flag-lang-"+time.Now().Format("150405.000000")).Scan(&person); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO role_grant (person_id, role, language) VALUES ($1, 'glossary keeper', 'ta')`, person); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `UPDATE role_grant SET ended_at = now() WHERE person_id = $1`, person)
	})
	got, err = people.LanguagesWithoutKeeper(ctx, pool)
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	for _, l := range got {
		if l == "ta" {
			t.Fatal(got)
		}
	}
}
