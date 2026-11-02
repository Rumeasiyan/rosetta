package sources

import (
	"context"
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
