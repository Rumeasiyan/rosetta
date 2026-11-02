package glossary

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/sheet"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/jackc/pgx/v5"
)

// The columns of the glossary's spreadsheet file (R-GLO-07), in the order they are written. The
// first row names them; a file may hold them in any order, and others, which are not read.
var Columns = []string{"en", "si", "ta", "type", "note"}

// columnNames is what the first row may call each column.
var columnNames = map[string]string{"en": "en", "english": "en", "si": "si", "sinhala": "si", "ta": "ta", "tamil": "ta", "type": "type", "note": "note"}

// ErrNoColumns says the first row does not name the columns.
var ErrNoColumns = errors.New("glossary: the file's first row does not name the columns")

// RowRefusal is one row left out and why, as a message key: never its words.
type RowRefusal struct {
	Row  int
	Word string
}

// Imported is what an import did.
type Imported struct {
	ID       int64
	Rows     int
	Accepted int
	Refused  []RowRefusal
}

// importRow is one row that is to join.
type importRow struct {
	kind, note string
	texts      map[i18n.Lang]string
}

// rowRefusal is the message key for what is wrong with a row.
func rowRefusal(err error) string {
	for _, r := range []struct {
		err  error
		word string
	}{
		{ErrBadType, "glossaryBadType"}, {ErrBadText, "glossaryBadText"}, {ErrBadNote, "glossaryBadNote"},
		{ErrKeepDiffers, "glossaryKeepDiffers"}, {ErrDuplicate, "glossaryDuplicate"},
	} {
		if errors.Is(err, r.err) {
			return r.word
		}
	}
	return "glossaryNoTerms"
}

// Import adds a file's rows as proposals waiting for keepers (R-GLO-07). A row that cannot join is
// left out and counted, with its number and why, and the others go on. It is done in whole
// statements rather than row by row, so 5,000 rows hold a connection of the five for moments.
func Import(ctx context.Context, tx pgx.Tx, name string, rows []sheet.Row, by int64) (Imported, error) {
	if len(rows) == 0 {
		return Imported{}, ErrNoColumns
	}
	place := map[string]int{}
	for i, c := range rows[0].Cells {
		if col, ok := columnNames[strings.ToLower(strings.TrimSpace(c))]; ok {
			if _, seen := place[col]; !seen {
				place[col] = i
			}
		}
	}
	if _, ok := place["type"]; !ok {
		return Imported{}, ErrNoColumns
	}
	if !slices.ContainsFunc([]string{"en", "si", "ta"}, func(c string) bool { _, ok := place[c]; return ok }) {
		return Imported{}, ErrNoColumns
	}
	taken, err := publishedTerms(ctx, tx)
	if err != nil {
		return Imported{}, err
	}
	out := Imported{Rows: len(rows) - 1}
	var good []importRow
	for _, r := range rows[1:] {
		row, err := readRow(r, place, taken)
		if err != nil {
			out.Refused = append(out.Refused, RowRefusal{Row: r.Number, Word: rowRefusal(err)})
			continue
		}
		good = append(good, row)
	}
	out.Accepted = len(good)
	err = tx.QueryRow(ctx, `INSERT INTO glossary_import (file_name, rows, accepted, refused, imported_by) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		cleanFileName(name), out.Rows, out.Accepted, len(out.Refused), by).Scan(&out.ID)
	if err != nil {
		return out, err
	}
	return out, addAll(ctx, tx, good, out.ID, by)
}

// readRow turns one row into a proposal, refusing what Propose would, and what the file holds twice.
func readRow(r sheet.Row, place map[string]int, taken map[string]bool) (importRow, error) {
	if r.Formula {
		return importRow{}, ErrBadText
	}
	cell := func(col string) string {
		i, ok := place[col]
		if !ok || i >= len(r.Cells) {
			return ""
		}
		return sheet.Restored(r.Cells[i])
	}
	p, err := Proposal{Type: strings.ToLower(strings.TrimSpace(cell("type"))), Note: cell("note"),
		Texts: map[i18n.Lang]string{i18n.EN: cell("en"), i18n.SI: cell("si"), i18n.TA: cell("ta")}}.parse()
	if err != nil {
		return importRow{}, err
	}
	if len(p.Texts) == 0 {
		return importRow{}, ErrNoTerms
	}
	keys := make([]string, 0, len(p.Texts))
	for l, t := range p.Texts {
		key := termKey(l, t)
		if taken[key] {
			return importRow{}, ErrDuplicate
		}
		keys = append(keys, key)
	}
	for _, k := range keys {
		taken[k] = true
	}
	return importRow{kind: p.Type, note: p.Note, texts: p.Texts}, nil
}

func termKey(l i18n.Lang, text string) string { return string(l) + "\x00" + strings.ToLower(text) }

// publishedTerms is every published term, by language, as termKey writes it.
func publishedTerms(ctx context.Context, q Querier) (map[string]bool, error) {
	rows, err := q.Query(ctx, `SELECT t.language, t.text FROM glossary_term t JOIN glossary_entry e ON e.id = t.glossary_entry_id WHERE e.status = 'published'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var l, t string
		if err := rows.Scan(&l, &t); err != nil {
			return nil, err
		}
		out[termKey(i18n.Lang(l), t)] = true
	}
	return out, rows.Err()
}

// addAll writes the rows in two statements, numbers first, so terms know their entries.
func addAll(ctx context.Context, tx pgx.Tx, list []importRow, imp, by int64) error {
	if len(list) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT nextval(pg_get_serial_sequence('glossary_entry', 'id')) FROM generate_series(1, $1::int)`, len(list))
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return err
	}
	kinds, notes := make([]string, len(list)), make([]string, len(list))
	var termIDs []int64
	var termLangs, termTexts []string
	for i, r := range list {
		kinds[i], notes[i] = r.kind, r.note
		for _, l := range i18n.Langs {
			if t := r.texts[l]; t != "" {
				termIDs, termLangs, termTexts = append(termIDs, ids[i]), append(termLangs, string(l)), append(termTexts, t)
			}
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO glossary_entry (id, type, note, status, glossary_import_id, proposed_by) OVERRIDING SYSTEM VALUE
		SELECT i, k, n, 'proposed', $4::bigint, $5::bigint FROM unnest($1::bigint[], $2::text[], $3::text[]) x(i, k, n)`, ids, kinds, notes, imp, by); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO glossary_term (glossary_entry_id, language, text) SELECT * FROM unnest($1::bigint[], $2::text[], $3::text[])`, termIDs, termLangs, termTexts)
	return err
}

// cleanFileName is the name to keep: the last part, normalised, at most 200 characters, and nothing
// that holds a card or phone number. It is shown only on these screens and never sent to the audit.
func cleanFileName(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(translate.Normalise(strings.ToValidUTF8(name, "")))
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	if name == "" {
		return "unnamed"
	}
	return name
}

// Export is the published glossary as rows: its columns, then each entry (R-GLO-07).
func Export(ctx context.Context, q Querier) ([][]string, error) {
	list, err := collect(ctx, q, entrySQL+` WHERE e.status = 'published' ORDER BY e.id LIMIT $1`, exportLimit)
	if err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(list)+1)
	out = append(out, slices.Clone(Columns))
	for _, e := range list {
		out = append(out, []string{e.Term(i18n.EN).Text, e.Term(i18n.SI).Text, e.Term(i18n.TA).Text, e.Type, e.Note})
	}
	return out, nil
}

// exportLimit stands well above the glossary's capacity of 20,000 entries (R-NFR-01).
const exportLimit = 100000
