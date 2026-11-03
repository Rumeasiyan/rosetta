package memory

import (
	"context"
	"errors"

	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/jackc/pgx/v5"
)

// Querier is what reads run through: a pool or a transaction.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Served answers a translation's pieces from the memory (R-TRN-04): people's checked work, so it is
// served whatever the gate says (R-GAT-02). An entry marked for recheck is not served until a reviewer
// has confirmed or corrected it (R-MEM-04). What it answers holds the same stand-ins as the piece.
type Served struct{ Q Querier }

// Answer finds the approved text of this piece, if any.
func (s Served) Answer(ctx context.Context, l translate.Lookup) (translate.Hit, bool, error) {
	var text string
	err := s.Q.QueryRow(ctx, `SELECT approved_text FROM memory_entry
		WHERE source_hash = $1 AND from_language = $2 AND to_language = $3 AND state = 'served'`, translate.Hash(l.Normal), l.From, l.To).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return translate.Hit{}, false, nil
	}
	if err != nil {
		return translate.Hit{}, false, err
	}
	return translate.Hit{Text: text, Origin: translate.OriginMemory}, true, nil
}
