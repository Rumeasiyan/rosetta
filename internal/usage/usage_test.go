package usage_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/dbtest"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/usage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func system(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `INSERT INTO caller (tva_reference, name, tva_status, tva_checked_at) VALUES ($1, $1, 'live', now())
		ON CONFLICT (tva_reference) DO UPDATE SET name = EXCLUDED.name RETURNING id`, name).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func clean(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, sql := range []string{`DELETE FROM daily_total`, `DELETE FROM usage_record`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
}

func at(t *testing.T, pool *pgxpool.Pool, caller int64, when time.Time, ref string, r usage.Record) {
	t.Helper()
	ctx := context.Background()
	if _, err := usage.AddParts(ctx, pool, when); err != nil {
		t.Fatal(err)
	}
	r.Ref, r.CallerID = ref, caller
	if err := usage.Keep(ctx, pool, r); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE usage_record SET at = $2 WHERE request_ref = $1`, ref, when); err != nil {
		t.Fatal(err)
	}
}

func nightly(t *testing.T, pool *pgxpool.Pool, v settings.Values, logs *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		logger := slog.New(slog.NewJSONHandler(logs, nil))
		_, err := usage.Nightly(ctx, tx, v, logger, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// Nothing a caller typed is written: only words on the fixed lists.
func TestKeepWritesOnlyWordsOnTheFixedLists(t *testing.T) {
	pool := dbtest.Open(t)
	clean(t, pool)
	ctx := context.Background()
	id := system(t, pool, "USAGE-FIXED")
	const marker = "TYPEDBYACALLER"
	err := usage.Keep(ctx, pool, usage.Record{Ref: "r-fixed-1", CallerID: id, Operation: usage.OpTranslate, Sensitivity: marker, From: marker, To: "EN", Outcome: "invalid request"})
	if err != nil {
		t.Fatal(err)
	}
	var row string
	if err := pool.QueryRow(ctx, `SELECT u::text FROM usage_record u WHERE request_ref = 'r-fixed-1'`).Scan(&row); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, marker) || strings.Contains(row, "EN") {
		t.Fatal(row)
	}
}

// A total is the sum of its records, and calling again changes nothing (R-REC-04; a run may repeat).
func TestNightlyTotalsAndRepeats(t *testing.T) {
	pool := dbtest.Open(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	ctx := context.Background()
	id := system(t, pool, "USAGE-TOTALS")
	loc, _ := time.LoadLocation("Asia/Colombo")
	y := time.Now().In(loc).AddDate(0, 0, -1)
	yesterday := time.Date(y.Year(), y.Month(), y.Day(), 10, 0, 0, 0, loc)
	// In Sri Lanka this is still yesterday, though it is another day in UTC by midnight.
	r := usage.Record{Operation: usage.OpTranslate, From: "en", To: "ta", Sensitivity: "ordinary", Passages: 1, Characters: 100, Pieces: usage.Pieces{Source: 2, Cache: 1}, Sources: []string{"site-a", "site-b"}, DurationMS: 1500, Mismatch: true, Outcome: usage.Answered}
	at(t, pool, id, yesterday, "r-a1", r)
	r.DurationMS, r.Sources, r.Mismatch = 70000, []string{"site-a"}, false
	at(t, pool, id, yesterday.Add(time.Hour), "r-a2", r)
	f := usage.Record{Operation: usage.OpTranslate, From: "en", To: "ta", Sensitivity: "ordinary", Outcome: "no provider"}
	at(t, pool, id, yesterday.Add(2*time.Hour), "r-a3", f)
	// Today's is not added up yet.
	at(t, pool, id, time.Now(), "r-a4", r)

	var logs bytes.Buffer
	check := func() {
		t.Helper()
		var requests, chars, source, cache, mismatches, under2, over int
		err := pool.QueryRow(ctx, `SELECT requests, characters, pieces_source, pieces_cache, mismatches, (time_buckets->>'under_2s')::int, (time_buckets->>'over_60s')::int
			FROM daily_total WHERE caller_id = $1 AND outcome = 'answered' AND source = ''`, id).Scan(&requests, &chars, &source, &cache, &mismatches, &under2, &over)
		if err != nil {
			t.Fatal(err)
		}
		if requests != 2 || chars != 200 || source != 4 || cache != 2 || mismatches != 1 || under2 != 1 || over != 1 {
			t.Fatal(requests, chars, source, cache, mismatches, under2, over)
		}
		var a, b int
		if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(requests) FILTER (WHERE source = 'site-a'), 0), COALESCE(sum(requests) FILTER (WHERE source = 'site-b'), 0) FROM daily_total WHERE caller_id = $1`, id).Scan(&a, &b); err != nil {
			t.Fatal(err)
		}
		if a != 2 || b != 1 {
			t.Fatal("by source", a, b)
		}
		var failed int
		if err := pool.QueryRow(ctx, `SELECT requests FROM daily_total WHERE caller_id = $1 AND outcome = 'no provider' AND source = ''`, id).Scan(&failed); err != nil {
			t.Fatal(err)
		}
		if failed != 1 {
			t.Fatal(failed)
		}
		var days int
		if err := pool.QueryRow(ctx, `SELECT count(DISTINCT day) FROM daily_total WHERE caller_id = $1`, id).Scan(&days); err != nil {
			t.Fatal(err)
		}
		if days != 1 {
			t.Fatalf("%d days added up: today's should wait", days)
		}
	}
	nightly(t, pool, settings.Defaults(), &logs)
	check()
	nightly(t, pool, settings.Defaults(), &logs)
	check()
}

// The oldest month goes whole, only when all of it is past the limit, and totals stay.
func TestRemovalsFollowTheSettings(t *testing.T) {
	pool := dbtest.Open(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	ctx := context.Background()
	id := system(t, pool, "USAGE-OLD")
	now := time.Now().UTC()
	old := time.Date(now.Year()-2, now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 5) // the month before two years ago
	recent := old.AddDate(0, 2, 0)                                                         // the month after two years ago
	// Parts for the old months, as they would have been made then.
	for _, when := range []time.Time{old, recent} {
		first := time.Date(when.Year(), when.Month(), 1, 0, 0, 0, 0, time.UTC)
		name := first.Format("usage_record_2006_01")
		if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+pgx.Identifier{name}.Sanitize()+` PARTITION OF usage_record FOR VALUES FROM ('`+first.Format(time.RFC3339)+`') TO ('`+first.AddDate(0, 1, 0).Format(time.RFC3339)+`')`); err != nil {
			t.Fatal(err)
		}
	}
	r := usage.Record{Operation: usage.OpRead, Outcome: usage.Answered}
	at(t, pool, id, old, "r-old", r)
	at(t, pool, id, recent, "r-recent", r)
	// A total older than five years goes; a total inside them stays.
	for _, days := range []int{5*366 + 40, 1500} {
		if _, err := pool.Exec(ctx, `INSERT INTO daily_total (day, caller_id, operation, outcome) VALUES (((now() AT TIME ZONE 'Asia/Colombo') - make_interval(days => $1))::date, $2, 'read', 'answered')`, days, id); err != nil {
			t.Fatal(err)
		}
	}
	// Expired and live cache; alerts and runs past 90 days, and a recent one of each.
	var source int64
	if err := pool.QueryRow(ctx, `INSERT INTO source (name, version, state, seen_at) VALUES ('usage-site', '1', 'on', now()) ON CONFLICT (name) DO UPDATE SET version = '1' RETURNING id`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM cache_piece WHERE source_id = $1`, source)
		pool.Exec(context.Background(), `DELETE FROM source WHERE id = $1`, source)
	})
	for i, off := range []string{"-1 hour", "+1 hour"} {
		if _, err := pool.Exec(ctx, `INSERT INTO cache_piece (source_hash, from_language, to_language, source_id, glossary_version, source_text, translation, source_version, expires_at)
			VALUES ($1, 'en', 'ta', $2, 0, 'invented', 'invented', '1', now() + $3::interval)`, []string{"usage-gone", "usage-stays"}[i], source, off); err != nil {
			t.Fatal(err)
		}
	}
	for _, sql := range []string{
		`INSERT INTO alert (kind, raised_at) VALUES ('usage old', now() - interval '91 days'), ('usage new', now())`,
		`INSERT INTO paradox_run (run_id, job, started_at) VALUES ('usage-run-old', 'nightly', now() - interval '91 days'), ('usage-run-new', 'nightly', now()) ON CONFLICT DO NOTHING`,
		`INSERT INTO audit_outbox (entry_id, occurred_at, action) VALUES ('usage-entry-old', now() - interval '31 days', 'denial'), ('usage-entry-new', now(), 'denial') ON CONFLICT DO NOTHING`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM alert WHERE kind LIKE 'usage %'`)
		pool.Exec(ctx, `DELETE FROM paradox_run WHERE run_id LIKE 'usage-run-%'`)
		pool.Exec(ctx, `DELETE FROM audit_outbox WHERE entry_id LIKE 'usage-entry-%'`)
	})

	var logs bytes.Buffer
	nightly(t, pool, settings.Defaults(), &logs)

	count := func(sql string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatal(sql, err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM usage_record WHERE request_ref = 'r-old'`); n != 0 {
		t.Error("the old month stayed")
	}
	if n := count(`SELECT count(*) FROM usage_record WHERE request_ref = 'r-recent'`); n != 1 {
		t.Error("a month inside the limit went")
	}
	// Each counted in totals before it went (a total of a day still held).
	if n := count(`SELECT count(*) FROM daily_total WHERE day >= now() - interval '6 years' AND requests > 0`); n < 2 {
		t.Errorf("%d totals", n)
	}
	if n := count(`SELECT count(*) FROM daily_total WHERE day < now() - make_interval(years => 5)`); n != 0 {
		t.Errorf("%d totals past five years", n)
	}
	if n := count(`SELECT count(*) FROM cache_piece WHERE source_hash = 'usage-gone'`); n != 0 {
		t.Error("expired cache stayed")
	}
	if n := count(`SELECT count(*) FROM cache_piece WHERE source_hash = 'usage-stays'`); n != 1 {
		t.Error("live cache went")
	}
	if n := count(`SELECT count(*) FROM alert WHERE kind LIKE 'usage %'`); n != 1 {
		t.Errorf("%d alerts", n)
	}
	if n := count(`SELECT count(*) FROM paradox_run WHERE run_id LIKE 'usage-run-%'`); n != 1 {
		t.Errorf("%d runs", n)
	}
	if n := count(`SELECT count(*) FROM audit_outbox WHERE entry_id LIKE 'usage-entry-%'`); n != 1 {
		t.Errorf("%d outbox rows", n)
	}
	if !strings.Contains(logs.String(), `"months_dropped":1`) {
		t.Errorf("%s", logs.String())
	}
}

// The job makes this month's part and the next ones, and a repeat makes none.
func TestAddPartsMakesWhatIsMissing(t *testing.T) {
	pool := dbtest.Open(t)
	ctx := context.Background()
	far := time.Date(2099, 11, 15, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		for _, n := range []string{"2099_11", "2099_12", "2100_01", "2100_02"} {
			pool.Exec(context.Background(), `DROP TABLE IF EXISTS usage_record_`+n)
		}
	})
	made, err := usage.AddParts(ctx, pool, far)
	if err != nil || made != usage.MonthsAhead+1 {
		t.Fatal(made, err)
	}
	made, err = usage.AddParts(ctx, pool, far)
	if err != nil || made != 0 {
		t.Fatal(made, err)
	}
}
