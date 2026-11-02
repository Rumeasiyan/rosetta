package sources

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is what reads run through: a pool or a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// storedState is the state as the table holds it: anything CALLERS.md does not list
// is not called, so it is kept as off.
func storedState(state string) string {
	switch state {
	case bumblebee.StateOn, bumblebee.StateRestingBlock, bumblebee.StateRestingChange:
		return state
	}
	return bumblebee.StateOff
}

// Mirror keeps the list in the source and source_offer tables, so gate decisions,
// orders and the cache have a row to refer to. A source gone from the list stays,
// as off, since those may still refer to it. The administrator's own switch
// (on_for_rosetta) is never touched.
func Mirror(ctx context.Context, pool *pgxpool.Pool, l bumblebee.List, at time.Time) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		names := make([]string, 0, len(l.Sources))
		for _, s := range l.Sources {
			names = append(names, s.Name)
			var id int64
			err := tx.QueryRow(ctx, `INSERT INTO source (name, version, state, rest_until, seen_at) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (name) DO UPDATE SET version = EXCLUDED.version, state = EXCLUDED.state,
					rest_until = EXCLUDED.rest_until, seen_at = EXCLUDED.seen_at
				RETURNING id`, s.Name, s.Version, storedState(s.State), s.RestUntil, at).Scan(&id)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM source_offer WHERE source_id = $1`, id); err != nil {
				return err
			}
			for _, o := range s.Offers {
				var to *string
				if o.Operation == bumblebee.OpTranslate {
					if o.To == o.From || o.To == "" {
						continue // not an offer the table or CALLERS.md allows
					}
					v := o.To
					to = &v
				}
				var score *float64
				if v, ok := s.Scores[o.From]; ok && o.Operation == bumblebee.OpGrammar {
					score = &v
				}
				if _, err := tx.Exec(ctx, `INSERT INTO source_offer (source_id, operation, from_language, to_language, benchmark_score)
					VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`, id, o.Operation, o.From, to, score); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(ctx, `UPDATE source SET state = 'off', rest_until = NULL, seen_at = $1 WHERE name <> ALL($2)`, at, names)
		return err
	})
}

// ErrUnknownSource says a source is not in the table: the list has not been read yet.
var ErrUnknownSource = errors.New("sources: no such source")

// LoadPolicy reads what the administrator has set.
func LoadPolicy(ctx context.Context, q Querier) (Policy, error) {
	p := Policy{Orders: map[Direction][]string{}, Off: map[string]bool{}}
	rows, err := q.Query(ctx, `SELECT o.operation, o.from_language, o.to_language, s.name FROM source_order o
		JOIN source s ON s.id = o.source_id ORDER BY o.operation, o.from_language, o.to_language, o.position`)
	if err != nil {
		return p, err
	}
	type row struct {
		d    Direction
		name string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.d.Operation, &x.d.From, &x.d.To, &x.name)
		return x, err
	})
	if err != nil {
		return p, err
	}
	for _, x := range list {
		p.Orders[x.d] = append(p.Orders[x.d], x.name)
	}
	rows, err = q.Query(ctx, `SELECT name FROM source WHERE NOT on_for_rosetta`)
	if err != nil {
		return p, err
	}
	off, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return p, err
	}
	for _, name := range off {
		p.Off[name] = true
	}
	return p, nil
}

// SetOrder replaces one direction's order, with its history row. It reports the old
// and new orders as text, and whether anything changed.
func SetOrder(ctx context.Context, tx pgx.Tx, d Direction, order []string, by int64) (before, after string, changed bool, err error) {
	var old []string
	rows, err := tx.Query(ctx, `SELECT s.name FROM source_order o JOIN source s ON s.id = o.source_id
		WHERE o.operation = $1 AND o.from_language = $2 AND o.to_language = $3 ORDER BY o.position FOR UPDATE OF o`, d.Operation, d.From, d.To)
	if err != nil {
		return "", "", false, err
	}
	if old, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return "", "", false, err
	}
	before, after = strings.Join(old, ", "), strings.Join(order, ", ")
	if before == after {
		return before, after, false, nil
	}
	if _, err = tx.Exec(ctx, `DELETE FROM source_order WHERE operation = $1 AND from_language = $2 AND to_language = $3`, d.Operation, d.From, d.To); err != nil {
		return "", "", false, err
	}
	for i, name := range order {
		tag, err := tx.Exec(ctx, `INSERT INTO source_order (operation, from_language, to_language, position, source_id, set_by)
			SELECT $1, $2, $3, $4, id, $6 FROM source WHERE name = $5`, d.Operation, d.From, d.To, i+1, name, by)
		if err != nil {
			return "", "", false, err
		}
		if tag.RowsAffected() != 1 {
			return "", "", false, ErrUnknownSource
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_history (subject, subject_ref, old_value, new_value, changed_by)
		VALUES ('source order', $1, $2, $3, $4)`, d.String(), before, after, by)
	return before, after, err == nil, err
}

// SetSwitch turns a source on or off for ROSETTA, with its history row. It reports
// whether anything changed.
func SetSwitch(ctx context.Context, tx pgx.Tx, name string, on bool, by int64) (bool, error) {
	var was bool
	err := tx.QueryRow(ctx, `SELECT on_for_rosetta FROM source WHERE name = $1 FOR UPDATE`, name).Scan(&was)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrUnknownSource
	}
	if err != nil || was == on {
		return false, err
	}
	if _, err = tx.Exec(ctx, `UPDATE source SET on_for_rosetta = $2, switched_at = now(), switched_by = $3 WHERE name = $1`, name, on, by); err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO change_history (subject, subject_ref, old_value, new_value, changed_by)
		VALUES ('source switch', $1, $2, $3, $4)`, name, strconv.FormatBool(was), strconv.FormatBool(on), by)
	return err == nil, err
}

// HistoryRow is one change to an order or a switch.
type HistoryRow struct {
	Subject   string
	Ref       string
	Old, New  string
	ChangedAt time.Time
	By        string
}

// HistoryLimit is the most rows the screen shows.
const HistoryLimit = 200

// History is the latest changes to orders and switches (R-ADM-05).
func History(ctx context.Context, q Querier) ([]HistoryRow, error) {
	rows, err := q.Query(ctx, `SELECT h.subject, h.subject_ref, COALESCE(h.old_value, ''), COALESCE(h.new_value, ''), h.changed_at, p.name
		FROM change_history h JOIN person p ON p.id = h.changed_by
		WHERE h.subject IN ('source order', 'source switch') ORDER BY h.id DESC LIMIT $1`, HistoryLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (HistoryRow, error) {
		var x HistoryRow
		err := r.Scan(&x.Subject, &x.Ref, &x.Old, &x.New, &x.ChangedAt, &x.By)
		return x, err
	})
}
