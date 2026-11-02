package usage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The ways a view is grouped (R-REC-03).
const (
	ByDay       = "day"
	BySystem    = "system"
	ByOperation = "operation"
	ByDirection = "direction"
	BySource    = "source"
)

// Groupings is every way, in the screens' order.
var Groupings = []string{ByDay, BySystem, ByOperation, ByDirection, BySource}

// Filter is one view. Dates are Sri Lanka's, Until included. Callers limits it to these systems: a
// system owner's own, never left nil for them (R-REC-03); nil is every system, for the
// administrator.
type Filter struct {
	Group        string
	Since, Until time.Time
	Callers      []int64
	Caller       int64 // one system, or 0
	Operation    string
	From, To     string
	Source       string
	AllCallers   bool // Callers does not apply
}

// Row is one line of a view: what R-REC-03 shows, for one day, system, operation, direction or source.
type Row struct {
	Key        string
	Name       string // the system's name, when grouped by system
	Requests   int64
	Answered   int64
	Characters int64
	Pieces     struct{ Glossary, Memory, Cache, Source int64 }
	Mismatches int64
	Buckets    []int64 // in the order of Buckets
	Reasons    map[string]int64
}

// Failed is the requests not answered.
func (r Row) Failed() int64 { return r.Requests - r.Answered }

// Slowest95 is the bucket that 95 of each 100 requests finished within, as a name from Buckets.
func (r Row) Slowest95() string {
	if r.Requests == 0 {
		return ""
	}
	var seen int64
	for i, n := range r.Buckets {
		seen += n
		if seen*100 >= r.Requests*95 {
			return Buckets[i].Name
		}
	}
	return Buckets[len(Buckets)-1].Name
}

func (r *Row) add(o Row) {
	r.Requests += o.Requests
	r.Answered += o.Answered
	r.Characters += o.Characters
	r.Pieces.Glossary += o.Pieces.Glossary
	r.Pieces.Memory += o.Pieces.Memory
	r.Pieces.Cache += o.Pieces.Cache
	r.Pieces.Source += o.Pieces.Source
	r.Mismatches += o.Mismatches
	if r.Buckets == nil {
		r.Buckets = make([]int64, len(Buckets))
	}
	for i := range o.Buckets {
		r.Buckets[i] += o.Buckets[i]
	}
	if r.Reasons == nil {
		r.Reasons = map[string]int64{}
	}
	for k, v := range o.Reasons {
		r.Reasons[k] += v
	}
}

// View is the lines of a view and the whole of it.
type View struct {
	Rows  []Row
	Whole Row
}

// ErrBadFilter says a view cannot be made. It repeats nothing it was given.
var ErrBadFilter = errors.New("usage: not a view that can be made")

// groupColumn is the column a view groups on. Nothing from the address reaches the SQL but these.
func groupColumn(group string) (string, error) {
	switch group {
	case ByDay:
		return `to_char(s.day, 'YYYY-MM-DD')`, nil
	case BySystem:
		return `c.tva_reference`, nil
	case ByOperation:
		return `s.operation`, nil
	case ByDirection:
		return `s.from_language || ' ' || s.to_language`, nil
	case BySource:
		return `s.source`, nil
	}
	return "", ErrBadFilter
}

// Make reads one view. What the totals hold comes from them, and what they do not yet hold is added
// up from the records in the same shape, so today shows too (R-REC-03).
func Make(ctx context.Context, q Querier, f Filter) (View, error) {
	column, err := groupColumn(f.Group)
	if err != nil {
		return View{}, err
	}
	if f.Until.Before(f.Since) {
		return View{}, ErrBadFilter
	}
	if !f.AllCallers && len(f.Callers) == 0 {
		return View{}, nil
	}
	args := []any{f.Since, f.Until}
	where := []string{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	// A row for the whole, unless a source is what is being looked at: then the rows naming it,
	// and a request with two sources counts under each.
	if f.Group == BySource || f.Source != "" {
		where = append(where, `s.source <> ''`)
		if f.Source != "" {
			add(`s.source = $%d`, f.Source)
		}
	} else {
		where = append(where, `s.source = ''`)
	}
	if !f.AllCallers {
		add(`s.caller_id = ANY($%d)`, f.Callers)
	}
	if f.Caller != 0 {
		add(`s.caller_id = $%d`, f.Caller)
	}
	if f.Operation != "" {
		add(`s.operation = $%d`, f.Operation)
	}
	if f.From != "" {
		add(`s.from_language = $%d`, f.From)
	}
	if f.To != "" {
		add(`s.to_language = $%d`, f.To)
	}
	buckets := make([]string, len(Buckets))
	for i, b := range Buckets {
		buckets[i] = fmt.Sprintf(`COALESCE(sum((s.time_buckets->>'%s')::bigint), 0)`, b.Name)
	}
	sql := `WITH known AS (SELECT max(day) AS last FROM daily_total),
		live AS (` + strings.ReplaceAll(strings.ReplaceAll(recordsAdded, "$1", `GREATEST($1::date, (SELECT COALESCE(last + 1, $1::date) FROM known))::timestamp AT TIME ZONE 'Asia/Colombo'`),
		"$2", `($2::date + 1)::timestamp AT TIME ZONE 'Asia/Colombo'`) + `),
		src AS (SELECT day, caller_id, operation, from_language, to_language, outcome, source, requests, characters, pieces_glossary, pieces_memory, pieces_cache, pieces_source, mismatches, time_buckets
				FROM daily_total WHERE day >= $1::date AND day <= $2::date
			UNION ALL SELECT day, caller_id, operation, from_language, to_language, outcome, source, requests, characters, pieces_glossary, pieces_memory, pieces_cache, pieces_source, mismatches, time_buckets FROM live)
		SELECT ` + column + ` AS key, COALESCE(max(c.name), ''), s.outcome, sum(s.requests), sum(s.characters), sum(s.pieces_glossary), sum(s.pieces_memory), sum(s.pieces_cache), sum(s.pieces_source), sum(s.mismatches), ` + strings.Join(buckets, ", ") + `
		FROM src s JOIN caller c ON c.id = s.caller_id
		WHERE ` + strings.Join(where, " AND ") + `
		GROUP BY key, s.outcome ORDER BY key`
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return View{}, err
	}
	defer rows.Close()
	byKey := map[string]*Row{}
	var keys []string
	for rows.Next() {
		var key, name, outcome string
		var x Row
		var numbers [7]int64
		x.Buckets = make([]int64, len(Buckets))
		dest := []any{&key, &name, &outcome}
		for i := range numbers {
			dest = append(dest, &numbers[i])
		}
		for i := range x.Buckets {
			dest = append(dest, &x.Buckets[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return View{}, err
		}
		x.Requests, x.Characters, x.Pieces.Glossary, x.Pieces.Memory, x.Pieces.Cache, x.Pieces.Source, x.Mismatches = numbers[0], numbers[1], numbers[2], numbers[3], numbers[4], numbers[5], numbers[6]
		if outcome == Answered {
			x.Answered = x.Requests
		} else {
			x.Reasons = map[string]int64{outcome: x.Requests}
		}
		line, ok := byKey[key]
		if !ok {
			line = &Row{Key: key, Name: name}
			byKey[key] = line
			keys = append(keys, key)
		}
		line.add(x)
	}
	if err := rows.Err(); err != nil {
		return View{}, err
	}
	out := View{Rows: make([]Row, 0, len(keys))}
	if f.Group == ByDay {
		slices.Reverse(keys) // newest first
	}
	for _, k := range keys {
		out.Rows = append(out.Rows, *byKey[k])
		out.Whole.add(*byKey[k])
	}
	if out.Whole.Buckets == nil {
		out.Whole.Buckets = make([]int64, len(Buckets))
	}
	return out, nil
}

// Today is today's requests by outcome, in Sri Lanka's day (R-ADM-02). Callers limits it as in a
// Filter, and all says it does not apply.
func Today(ctx context.Context, q Querier, callers []int64, all bool) (map[string]int64, error) {
	rows, err := q.Query(ctx, `SELECT outcome, count(*) FROM usage_record WHERE at >= `+startOfToday+`
		AND ($1 OR caller_id = ANY($2)) GROUP BY outcome`, all, callers)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	var outcome string
	var n int64
	if _, err := pgx.ForEachRow(rows, []any{&outcome, &n}, func() error { out[outcome] = n; return nil }); err != nil {
		return nil, err
	}
	return out, nil
}
