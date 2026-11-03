package usage

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

// Buckets is the time taken, in the counts a daily total keeps (Annex A: time_buckets). R-NFR-02
// and R-ERR-03 name these edges; a row holds how many requests fell in each.
var Buckets = []Bucket{
	{"under_1s", 0, 1000}, {"under_2s", 1000, 2000}, {"under_6s", 2000, 6000},
	{"under_15s", 6000, 15000}, {"under_60s", 15000, 60000}, {"over_60s", 60000, 0},
}

// Bucket is one span of time taken, in milliseconds; To 0 has no top.
type Bucket struct {
	Name     string
	From, To int
}

// bucketsSQL builds the JSON a total holds, from the record's own time.
func bucketsSQL() string {
	out := "jsonb_build_object("
	for i, b := range Buckets {
		if i > 0 {
			out += ", "
		}
		cond := fmt.Sprintf("u.duration_ms >= %d", b.From)
		if b.To > 0 {
			cond += fmt.Sprintf(" AND u.duration_ms < %d", b.To)
		}
		out += fmt.Sprintf("'%s', count(*) FILTER (WHERE %s)", b.Name, cond)
	}
	return out + ")"
}

// recordsAdded is records between two times in the shape of a daily total, with a row for the whole
// and one for each source: the one definition the nightly job and the screens both use. The day is
// Sri Lanka's, as everywhere on the screens.
var recordsAdded = `SELECT (u.at AT TIME ZONE 'Asia/Colombo')::date AS day, u.caller_id, u.operation, COALESCE(u.from_language, '') AS from_language,
		COALESCE(u.to_language, '') AS to_language, u.outcome, s.source, count(*) AS requests, COALESCE(sum(u.characters), 0) AS characters,
		COALESCE(sum(u.pieces_glossary), 0) AS pieces_glossary, COALESCE(sum(u.pieces_memory), 0) AS pieces_memory, COALESCE(sum(u.pieces_cache), 0) AS pieces_cache,
		COALESCE(sum(u.pieces_source), 0) AS pieces_source, count(*) FILTER (WHERE u.mismatch) AS mismatches, ` + bucketsSQL() + ` AS time_buckets
	FROM usage_record u CROSS JOIN LATERAL (SELECT '' AS source UNION ALL SELECT unnest(u.sources_used)) s
	WHERE u.at >= $1 AND u.at < $2 GROUP BY 1, 2, 3, 4, 5, 6, 7`

const startOfToday = `date_trunc('day', now() AT TIME ZONE 'Asia/Colombo') AT TIME ZONE 'Asia/Colombo'`

// AddTotals adds up what the totals do not yet hold, to the end of yesterday in Sri Lanka
// (R-REC-04). The last day held is added again, so a call sent twice or a late record comes out
// the same: a total is always the sum of its records, never added to itself. It reports the rows.
func AddTotals(ctx context.Context, tx pgx.Tx) (int, error) {
	var from, to time.Time
	var known *time.Time
	if err := tx.QueryRow(ctx, `SELECT max(day)::timestamp AT TIME ZONE 'Asia/Colombo', `+startOfToday+` FROM daily_total`).Scan(&known, &to); err != nil {
		return 0, err
	}
	if known != nil {
		from = *known
	}
	tag, err := tx.Exec(ctx, `INSERT INTO daily_total (day, caller_id, operation, from_language, to_language, outcome, source, requests, characters,
			pieces_glossary, pieces_memory, pieces_cache, pieces_source, mismatches, time_buckets) `+recordsAdded+`
		ON CONFLICT (day, caller_id, operation, from_language, to_language, outcome, source) DO UPDATE SET
			requests = EXCLUDED.requests, characters = EXCLUDED.characters, pieces_glossary = EXCLUDED.pieces_glossary, pieces_memory = EXCLUDED.pieces_memory,
			pieces_cache = EXCLUDED.pieces_cache, pieces_source = EXCLUDED.pieces_source, mismatches = EXCLUDED.mismatches, time_buckets = EXCLUDED.time_buckets`, from, to)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

var partName = regexp.MustCompile(`^usage_record_(\d{4})_(\d{2})$`)

// DropOldParts removes each month of records wholly older than this many years, the oldest month
// going whole (Annex A.1). So a record may stay for up to a month past the limit. It reports how
// many months went. Run it after AddTotals, in the same transaction, so nothing is lost uncounted.
func DropOldParts(ctx context.Context, tx pgx.Tx, years int, now time.Time) (int, error) {
	rows, err := tx.Query(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid WHERE i.inhparent = 'usage_record'::regclass`)
	if err != nil {
		return 0, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	cutoff := now.UTC().AddDate(-years, 0, 0)
	dropped := 0
	for _, name := range names {
		m := partName.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		start, err := time.Parse("2006-01", m[1]+"-"+m[2])
		if err != nil {
			continue
		}
		if start.AddDate(0, 1, 0).After(cutoff) {
			continue
		}
		if _, err := tx.Exec(ctx, `DROP TABLE `+pgx.Identifier{name}.Sanitize()); err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

// DropOldTotals removes daily totals older than this many years (R-REC-04).
func DropOldTotals(ctx context.Context, tx pgx.Tx, years int) (int, error) {
	tag, err := tx.Exec(ctx, `DELETE FROM daily_total WHERE day < ((now() AT TIME ZONE 'Asia/Colombo') - make_interval(years => $1))::date`, years)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// MonthsAhead is how many months past this one have their part ready.
const MonthsAhead = 3

// AddParts makes this month's part and the next ones, for the job and at each start. A part that
// exists stays as it is. It reports how many were made.
func AddParts(ctx context.Context, q Querier, now time.Time) (int, error) {
	first := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	made := 0
	for i := 0; i <= MonthsAhead; i++ {
		start := first.AddDate(0, i, 0)
		name := start.Format("usage_record_2006_01")
		var exists bool
		if err := q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
			return made, err
		}
		if exists {
			continue
		}
		sql := fmt.Sprintf(`CREATE TABLE %s PARTITION OF usage_record FOR VALUES FROM ('%s') TO ('%s')`, pgx.Identifier{name}.Sanitize(),
			start.Format(time.RFC3339), start.AddDate(0, 1, 0).Format(time.RFC3339))
		if _, err := q.Exec(ctx, sql); err != nil {
			return made, err
		}
		made++
	}
	return made, nil
}
