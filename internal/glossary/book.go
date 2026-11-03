package glossary

import (
	"context"
	"sync"

	"github.com/Rumeasiyan/rosetta/internal/terms"
	"github.com/jackc/pgx/v5"
)

// Book holds the published terms in memory, so a request finds them with no query for each piece.
// The version is read on every request, as every answer must state it (R-TRN-06); the terms are read
// again when it has moved, so a change is in force on the next request.
type Book struct {
	Q Querier

	mu      sync.Mutex
	loaded  bool
	version int
	rows    []bookRow
	sets    map[[2]string]*terms.Set
}

type bookRow struct {
	en, si, ta string
}

func (r bookRow) in(language string) string {
	switch language {
	case "si":
		return r.si
	case "ta":
		return r.ta
	}
	return r.en
}

// Terms is the version now and its terms from one language to another. From a language to itself it
// is the terms of that language, as the grammar check needs them. An error says the glossary could
// not be read; nothing is answered from an unknown glossary.
func (b *Book) Terms(ctx context.Context, from, to string) (int, *terms.Set, error) {
	version, err := Current(ctx, b.Q)
	if err != nil {
		return 0, nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.loaded || version != b.version {
		if err := b.load(ctx); err != nil {
			return 0, nil, err
		}
	}
	key := [2]string{from, to}
	set, ok := b.sets[key]
	if !ok {
		list := make([]terms.Term, 0, len(b.rows))
		for _, r := range b.rows {
			list = append(list, terms.Term{Source: r.in(from), Target: r.in(to)})
		}
		set = terms.New(list)
		b.sets[key] = set
	}
	return b.version, set, nil
}

// load reads the version and the terms in one statement, so they always belong together.
func (b *Book) load(ctx context.Context) error {
	rows, err := b.Q.Query(ctx, `SELECT (SELECT COALESCE(max(number), 0) FROM glossary_version),
			COALESCE((SELECT text FROM glossary_term t WHERE t.glossary_entry_id = e.id AND t.language = 'en'), ''),
			COALESCE((SELECT text FROM glossary_term t WHERE t.glossary_entry_id = e.id AND t.language = 'si'), ''),
			COALESCE((SELECT text FROM glossary_term t WHERE t.glossary_entry_id = e.id AND t.language = 'ta'), '')
		FROM (SELECT id FROM glossary_entry WHERE status = 'published' UNION ALL SELECT NULL WHERE NOT EXISTS (SELECT 1 FROM glossary_entry WHERE status = 'published')) e ORDER BY e.id`)
	if err != nil {
		return err
	}
	version := 0
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (bookRow, error) {
		var x bookRow
		err := r.Scan(&version, &x.en, &x.si, &x.ta)
		return x, err
	})
	if err != nil {
		return err
	}
	kept := list[:0]
	for _, x := range list {
		if x.en != "" || x.si != "" || x.ta != "" {
			kept = append(kept, x)
		}
	}
	b.rows, b.version, b.loaded, b.sets = kept, version, true, map[[2]string]*terms.Set{}
	return nil
}
