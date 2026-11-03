package memory

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Refusals on the screens. None holds any of the text.
var (
	ErrNoSuchEntry = errors.New("memory: no such entry")
	ErrNotRecheck  = errors.New("memory: this entry is not waiting for a recheck")
	ErrGone        = errors.New("memory: this entry has been removed or replaced")
	ErrBadText     = errors.New("memory: not a text this entry may hold")
)

// PageLimit is the most entries one page shows (R-NFR-03).
const PageLimit = 100

// Writer is what a change runs through: a transaction.
type Writer interface {
	Querier
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Entry is one memory entry as a reviewer sees it.
type Entry struct {
	ID              int64
	From, To        string
	Source          string
	Approved        string
	ApproverPost    string
	ApprovedOn      time.Time
	State           string
	System          string
	GlossaryVersion int
	AddedAt         time.Time
	ReviewedAt      *time.Time
	ReviewedBy      string
	RecheckEntry    *int64
	RecheckVersion  *int
}

// FromLang and ToLang are its languages, as the screens name them.
func (e Entry) FromLang() i18n.Lang { return i18n.Lang(e.From) }

// ToLang is the language it is served in.
func (e Entry) ToLang() i18n.Lang { return i18n.Lang(e.To) }

const entrySQL = `SELECT m.id, m.from_language, m.to_language, m.source_text, m.approved_text, m.approver_post, m.approved_on, m.state, c.name, m.glossary_version,
		m.added_at, m.reviewed_at, COALESCE(p.name, ''), m.recheck_glossary_entry_id, m.recheck_glossary_version
	FROM memory_entry m JOIN caller c ON c.id = m.caller_id LEFT JOIN person p ON p.id = m.reviewed_by`

func scanEntry(r pgx.Row) (Entry, error) {
	var e Entry
	err := r.Scan(&e.ID, &e.From, &e.To, &e.Source, &e.Approved, &e.ApproverPost, &e.ApprovedOn, &e.State, &e.System, &e.GlossaryVersion, &e.AddedAt, &e.ReviewedAt, &e.ReviewedBy, &e.RecheckEntry, &e.RecheckVersion)
	return e, err
}

func collect(ctx context.Context, q Querier, sql string, args ...any) ([]Entry, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
}

// Get is one entry.
func Get(ctx context.Context, q Querier, id int64) (Entry, error) {
	e, err := scanEntry(q.QueryRow(ctx, entrySQL+` WHERE m.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNoSuchEntry
	}
	return e, err
}

// like is a pattern finding s anywhere, with its own % and _ as written.
func like(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

// languageFilter is the SQL for "in these languages"; no list means every language, the administrator's.
const languageFilter = `(cardinality($1::text[]) = 0 OR m.to_language = ANY($1::text[]))`

// Find is the live entries holding text on either side, normalised as they are (R-MEM-05, R-NFR-05).
func Find(ctx context.Context, q Querier, text string, languages []string) ([]Entry, error) {
	text = translate.Normalise(strings.TrimSpace(text))
	if text == "" {
		return nil, nil
	}
	return collect(ctx, q, entrySQL+` WHERE m.state IN ('served', 'recheck') AND `+languageFilter+
		` AND (m.source_text ILIKE $2 OR m.approved_text ILIKE $2) ORDER BY m.id DESC LIMIT $3`, languages, like(text), PageLimit)
}

// Rechecks is what waits for a reviewer in these languages, oldest first, and how many in all.
func Rechecks(ctx context.Context, q Querier, languages []string) ([]Entry, int, error) {
	var total int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM memory_entry m WHERE m.state = 'recheck' AND `+languageFilter, languages).Scan(&total); err != nil {
		return nil, 0, err
	}
	list, err := collect(ctx, q, entrySQL+` WHERE m.state = 'recheck' AND `+languageFilter+` ORDER BY m.id LIMIT $2`, languages, PageLimit)
	return list, total, err
}

// lockEntry reads an entry's state with its row held, so two reviewers cannot cross.
func lockEntry(ctx context.Context, tx Writer, id int64) (string, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM memory_entry WHERE id = $1 FOR UPDATE`, id).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoSuchEntry
	}
	return state, err
}

// Confirm is a reviewer saying a marked entry is still right: it is served again (R-MEM-04).
func Confirm(ctx context.Context, tx Writer, id, by int64) error {
	state, err := lockEntry(ctx, tx, id)
	if err != nil {
		return err
	}
	if state != StateRecheck {
		return ErrNotRecheck
	}
	_, err = tx.Exec(ctx, `UPDATE memory_entry SET state = 'served', reviewed_at = now(), reviewed_by = $2, recheck_glossary_entry_id = NULL, recheck_glossary_version = NULL WHERE id = $1`, id, by)
	return err
}

// CleanCorrection checks a reviewer's words against the entry they correct: not blank, within a
// passage's length, holding no number masking would take, and every stand-in of the source once, so
// each number can still be restored. Its result is what is stored (R-NFR-05).
func CleanCorrection(source, text string, v settings.Values) (string, error) {
	if !utf8.ValidString(text) {
		return "", ErrBadText
	}
	text = translate.Normalise(strings.TrimSpace(text))
	if !strings.ContainsFunc(mask.WithoutStandIns(text), unicode.IsLetter) || utf8.RuneCountInString(text) > v.Of(settings.PassageCharsMax) {
		return "", ErrBadText
	}
	if strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) || len(mask.Mask(text).Items) > 0 {
		return "", ErrBadText
	}
	want, got := mask.StandIns(source), mask.StandIns(text)
	if len(want) != len(got) {
		return "", ErrBadText
	}
	for _, t := range want {
		if strings.Count(text, t) != 1 {
			return "", ErrBadText
		}
	}
	return text, nil
}

// Correct is a reviewer putting a marked entry right: it is served again with their words, under the
// glossary version now (R-MEM-04). The approver's post stays the calling system's own record, and
// who corrected it is the reviewer and the audit entry.
func Correct(ctx context.Context, tx Writer, id int64, text string, by int64, v settings.Values) error {
	state, err := lockEntry(ctx, tx, id)
	if err != nil {
		return err
	}
	if state != StateRecheck {
		return ErrNotRecheck
	}
	var source string
	if err := tx.QueryRow(ctx, `SELECT source_text FROM memory_entry WHERE id = $1`, id).Scan(&source); err != nil {
		return err
	}
	clean, err := CleanCorrection(source, text, v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE memory_entry SET state = 'served', approved_text = $2, reviewed_at = now(), reviewed_by = $3, recheck_glossary_entry_id = NULL, recheck_glossary_version = NULL,
		glossary_version = (SELECT max(number) FROM glossary_version) WHERE id = $1`, id, clean, by)
	return err
}

// Remove takes a wrong entry out (R-MEM-04, R-MEM-05): its words go, and the row keeps who and when.
func Remove(ctx context.Context, tx Writer, id, by int64) error {
	state, err := lockEntry(ctx, tx, id)
	if err != nil {
		return err
	}
	if state != StateServed && state != StateRecheck {
		return ErrGone
	}
	_, err = tx.Exec(ctx, `UPDATE memory_entry SET state = 'removed', source_text = '', approved_text = '', removed_at = now(), removed_by = $2,
		recheck_glossary_entry_id = NULL, recheck_glossary_version = NULL WHERE id = $1`, id, by)
	return err
}
