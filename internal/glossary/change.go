package glossary

import (
	"context"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/jackc/pgx/v5"
)

// Change is what a publication did to what was made under the old glossary (R-GLO-06).
type Change struct {
	CacheRemoved int
	MemoryMarked int
}

// MaxMarks is the most memory entries one publication may mark. Marking is written to every index of a
// row, and the platform stops any statement at 30 seconds (allspark apps/_template/database.sql), which
// over a full memory is about 10,000 rows at worst, as measured (docs/DECISION-LOG.md). A term held by
// more than this is too common a word to be a term, so the publication is refused whole rather than
// timing out as a fault. E-NFR-01 measures again.
const MaxMarks = 10000

// termsByLanguage is one entry's words.
type termsByLanguage map[i18n.Lang]string

// patternsOf is each different text of the two entries in this language, as patterns.
func patternsOf(l i18n.Lang, was, now termsByLanguage) []string {
	var out []string
	if t := now[l]; t != "" {
		out = append(out, Like(t))
	}
	if t := was[l]; t != "" && t != now[l] {
		out = append(out, Like(t))
	}
	return out
}

// applyChange runs in the publishing transaction. For each language whose term changed, a cached piece
// is removed and a memory entry marked for recheck if it holds the term there, old or new, or is a
// translation into that language of a text holding the term in its own (R-GLO-06). So correcting the
// Tamil leaves an English to Sinhala entry alone. A cached piece that went to a source with the term stood in
// for holds no term and stays: it comes back with the new one. The pieces are found with pg_trgm,
// which is substring matching, so a longer word holding a term is marked too: rechecking one needlessly
// costs a person a moment, and a wrong approved text being served costs more.
func applyChange(ctx context.Context, tx pgx.Tx, entry int64, version int, was, now termsByLanguage) (Change, error) {
	var out Change
	counted := 0
	for _, l := range i18n.Langs {
		if was[l] == now[l] {
			continue
		}
		own := patternsOf(l, was, now)
		// The other two languages' texts of this entry, each only against translations into l.
		var others []i18n.Lang
		for _, m := range i18n.Langs {
			if m != l {
				others = append(others, m)
			}
		}
		first, second := patternsOf(others[0], was, now), patternsOf(others[1], was, now)
		var held int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM memory_entry WHERE state = 'served' AND ((from_language = $1 AND source_text ILIKE ANY($2::text[]))
			OR (to_language = $1 AND approved_text ILIKE ANY($2::text[]))
			OR (to_language = $1 AND from_language = $3 AND source_text ILIKE ANY($4::text[]))
			OR (to_language = $1 AND from_language = $5 AND source_text ILIKE ANY($6::text[]))) LIMIT $7) x`,
			string(l), own, string(others[0]), first, string(others[1]), second, MaxMarks+1).Scan(&held); err != nil {
			return out, err
		}
		if counted += held; counted > MaxMarks {
			return Change{}, ErrTooCommon
		}
		tag, err := tx.Exec(ctx, `DELETE FROM cache_piece WHERE (from_language = $1 AND source_text ILIKE ANY($2::text[]))
			OR (to_language = $1 AND translation ILIKE ANY($2::text[]))
			OR (to_language = $1 AND from_language = $3 AND source_text ILIKE ANY($4::text[]))
			OR (to_language = $1 AND from_language = $5 AND source_text ILIKE ANY($6::text[]))`,
			string(l), own, string(others[0]), first, string(others[1]), second)
		if err != nil {
			return out, err
		}
		out.CacheRemoved += int(tag.RowsAffected())
		tag, err = tx.Exec(ctx, `UPDATE memory_entry SET state = 'recheck', recheck_glossary_entry_id = $7, recheck_glossary_version = $8
			WHERE state = 'served' AND ((from_language = $1 AND source_text ILIKE ANY($2::text[]))
			OR (to_language = $1 AND approved_text ILIKE ANY($2::text[]))
			OR (to_language = $1 AND from_language = $3 AND source_text ILIKE ANY($4::text[]))
			OR (to_language = $1 AND from_language = $5 AND source_text ILIKE ANY($6::text[])))`,
			string(l), own, string(others[0]), first, string(others[1]), second, entry, version)
		if err != nil {
			return out, err
		}
		out.MemoryMarked += int(tag.RowsAffected())
	}
	return out, nil
}

// wordsOf reads an entry's terms.
func wordsOf(ctx context.Context, q Querier, entry int64) (termsByLanguage, error) {
	rows, err := q.Query(ctx, `SELECT language, text FROM glossary_term WHERE glossary_entry_id = $1`, entry)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := termsByLanguage{}
	for rows.Next() {
		var l, t string
		if err := rows.Scan(&l, &t); err != nil {
			return nil, err
		}
		out[i18n.Lang(l)] = t
	}
	return out, rows.Err()
}
