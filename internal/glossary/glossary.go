// Package glossary is the one shared glossary (R-GLO): each entry holds the same term in
// Sinhala, Tamil and English, checked by the keeper for each language. It is published only
// when all three are checked, which raises the one version number by one (R-GLO-05). Entries
// are never deleted, only replaced, and what is published never changes: a correction is a
// new entry.
//
// Nothing here logs or returns a term in an error. All text passes translate.Normalise
// before it is compared or stored (R-NFR-05).
package glossary

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The types an entry has (R-GLO-01).
const (
	TypePost        = "post"
	TypeInstitution = "institution"
	TypePlace       = "place"
	TypeTerm        = "term"
	TypeKeep        = "keep as written"
)

// Types is every type, in the order the form offers them.
var Types = []string{TypePost, TypeInstitution, TypePlace, TypeTerm, TypeKeep}

// The states of an entry (Annex A).
const (
	StatusProposed  = "proposed"
	StatusHeld      = "held"
	StatusPublished = "published"
	StatusReplaced  = "replaced"
	StatusRefused   = "refused"
)

// Longest is what a term and a note may hold (Annex A), in characters.
const (
	maxTerm = 500
	maxNote = 500
)

// lockVersion keeps two publications from taking the same number.
const lockVersion = 0x52535402

// The refusals. None holds any of the text.
var (
	ErrBadType         = errors.New("glossary: not a type of entry")
	ErrBadText         = errors.New("glossary: not a text a term may be")
	ErrBadNote         = errors.New("glossary: not a note an entry may hold")
	ErrNoTerms         = errors.New("glossary: no term in any language")
	ErrKeepDiffers     = errors.New("glossary: an entry kept as written reads the same in every language")
	ErrDuplicate       = errors.New("glossary: another published entry holds this term")
	ErrNoSuchEntry     = errors.New("glossary: no such entry")
	ErrNotWaiting      = errors.New("glossary: this entry is not waiting for a keeper")
	ErrNotCurrent      = errors.New("glossary: only the published entry can be corrected")
	ErrCorrectionWaits = errors.New("glossary: a correction of this entry is already waiting")
	ErrNothingChanged  = errors.New("glossary: the correction changes nothing")
	ErrNoText          = errors.New("glossary: there is no text in this language to check")
	ErrAlreadyChecked  = errors.New("glossary: this language is already checked")
	ErrBadLanguage     = errors.New("glossary: not a language")
	ErrNoOutcomeNote   = errors.New("glossary: a refusal says why")
	ErrTooCommon       = errors.New("glossary: more memory entries hold this term than one publication may mark")
	ErrNoVersion       = errors.New("glossary: no such version")
)

// Querier is what these run through: a pool or a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Term is one language of an entry.
type Term struct {
	Language  i18n.Lang
	Text      string
	CheckedAt *time.Time
	CheckedBy string
}

// Entry is one glossary entry, with its terms in the order of i18n.Langs. A language
// nobody has written yet has an empty Text.
type Entry struct {
	ID           int64
	Type         string
	Note         string
	Status       string
	ReplacesID   *int64
	ReplacedByID *int64
	PublishedIn  *int
	ReplacedIn   *int
	ProposedAt   time.Time
	ProposedByID *int64
	ProposedBy   string
	OutcomeNote  string
	Terms        []Term
}

// Term is the entry's term in one language.
func (e Entry) Term(l i18n.Lang) Term {
	for _, t := range e.Terms {
		if t.Language == l {
			return t
		}
	}
	return Term{Language: l}
}

// Waiting says keepers can still act on it.
func (e Entry) Waiting() bool { return e.Status == StatusProposed || e.Status == StatusHeld }

// Unchecked is the languages still to be checked: the keepers concerned (R-GLO-04).
func (e Entry) Unchecked() []i18n.Lang {
	var out []i18n.Lang
	for _, t := range e.Terms {
		if t.CheckedAt == nil {
			out = append(out, t.Language)
		}
	}
	return out
}

// CleanTerm is a term as it is stored. A term begins with a letter or a digit, so it can be
// found in a text; it holds no line break; it holds nothing masking would take away, or the
// stand-ins' brackets, since it could then never be found (R-MSK-02).
func CleanTerm(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrBadText
	}
	s = translate.Normalise(strings.TrimSpace(s))
	if s == "" || utf8.RuneCountInString(s) > maxTerm || strings.ContainsAny(s, "⟦⟧") {
		return "", ErrBadText
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", ErrBadText
	}
	if first, _ := utf8.DecodeRuneInString(s); !unicode.IsLetter(first) && !unicode.IsDigit(first) {
		return "", ErrBadText
	}
	if len(mask.Mask(s).Items) > 0 {
		return "", ErrBadText
	}
	return s, nil
}

// CleanNote is a note or an outcome as it is stored; it may be empty.
func CleanNote(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrBadNote
	}
	s = translate.Normalise(strings.TrimSpace(s))
	if utf8.RuneCountInString(s) > maxNote || mask.Mask(s).HasCard() {
		return "", ErrBadNote
	}
	return s, nil
}

// Proposal is a new entry, or a correction of a published one when Replaces is set.
type Proposal struct {
	Type     string
	Note     string
	Texts    map[i18n.Lang]string // a blank language is for its keeper to write
	By       int64
	Replaces *int64
	ImportID *int64
}

// parse checks the proposal's words. An entry kept as written reads the same in all three,
// so one written is all written.
func (p Proposal) parse() (Proposal, error) {
	if !slices.Contains(Types, p.Type) {
		return p, ErrBadType
	}
	var err error
	if p.Note, err = CleanNote(p.Note); err != nil {
		return p, err
	}
	texts := map[i18n.Lang]string{}
	for _, l := range i18n.Langs {
		raw := strings.TrimSpace(p.Texts[l])
		if raw == "" {
			continue
		}
		if texts[l], err = CleanTerm(raw); err != nil {
			return p, err
		}
	}
	if p.Type == TypeKeep {
		kept := ""
		for _, l := range i18n.Langs {
			if t := texts[l]; t != "" {
				if kept != "" && kept != t {
					return p, ErrKeepDiffers
				}
				kept = t
			}
		}
		if kept != "" {
			for _, l := range i18n.Langs {
				texts[l] = kept
			}
		}
	}
	p.Texts = texts
	return p, nil
}

// Propose adds an entry waiting for keepers and says which languages they must check. A
// correction brings the replaced entry's words for any language left blank, and a language
// that reads as it did keeps its check; a change to the type or note alone is for all three.
func Propose(ctx context.Context, tx pgx.Tx, p Proposal) (int64, []i18n.Lang, error) {
	p, err := p.parse()
	if err != nil {
		return 0, nil, err
	}
	carried := map[i18n.Lang]Term{}
	if p.Replaces != nil {
		if carried, err = correcting(ctx, tx, &p); err != nil {
			return 0, nil, err
		}
	} else if len(p.Texts) == 0 {
		return 0, nil, ErrNoTerms
	}
	for _, l := range i18n.Langs {
		if t := p.Texts[l]; t != "" {
			if err := notTaken(ctx, tx, l, t, 0, p.Replaces); err != nil {
				return 0, nil, err
			}
		}
	}
	status := StatusProposed
	if len(carried) > 0 {
		status = StatusHeld
	}
	var id int64
	var by *int64
	if p.By != 0 {
		by = &p.By
	}
	err = tx.QueryRow(ctx, `INSERT INTO glossary_entry (type, note, status, replaces_id, glossary_import_id, proposed_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, p.Type, p.Note, status, p.Replaces, p.ImportID, by).Scan(&id)
	if err != nil {
		return 0, nil, err
	}
	var concerned []i18n.Lang
	for _, l := range i18n.Langs {
		_, kept := carried[l]
		if !kept {
			concerned = append(concerned, l)
		}
		text := p.Texts[l]
		if text == "" {
			continue
		}
		if kept {
			_, err = tx.Exec(ctx, `INSERT INTO glossary_term (glossary_entry_id, language, text, checked_at, checked_by)
				SELECT $1, language, text, checked_at, checked_by FROM glossary_term WHERE glossary_entry_id = $2 AND language = $3`, id, *p.Replaces, string(l))
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO glossary_term (glossary_entry_id, language, text) VALUES ($1, $2, $3)`, id, string(l), text)
		}
		if err != nil {
			return 0, nil, err
		}
	}
	return id, concerned, nil
}

// correcting fills a correction in from the entry it replaces, with that entry held so it
// cannot be replaced meanwhile, and returns the languages whose check carries over.
func correcting(ctx context.Context, tx pgx.Tx, p *Proposal) (map[i18n.Lang]Term, error) {
	var status, oldType, oldNote string
	err := tx.QueryRow(ctx, `SELECT status, type, note FROM glossary_entry WHERE id = $1 FOR UPDATE`, *p.Replaces).Scan(&status, &oldType, &oldNote)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSuchEntry
	}
	if err != nil {
		return nil, err
	}
	if status != StatusPublished {
		return nil, ErrNotCurrent
	}
	var waiting bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM glossary_entry WHERE replaces_id = $1 AND status IN ('proposed', 'held'))`, *p.Replaces).Scan(&waiting); err != nil {
		return nil, err
	}
	if waiting {
		return nil, ErrCorrectionWaits
	}
	old, err := Get(ctx, tx, *p.Replaces)
	if err != nil {
		return nil, err
	}
	carried := map[i18n.Lang]Term{}
	changed := false
	for _, t := range old.Terms {
		if p.Texts[t.Language] == "" {
			p.Texts[t.Language] = t.Text
		}
		if p.Texts[t.Language] == t.Text {
			carried[t.Language] = t
		} else {
			changed = true
		}
	}
	if !changed {
		if p.Type == oldType && p.Note == oldNote {
			return nil, ErrNothingChanged
		}
		return map[i18n.Lang]Term{}, nil
	}
	if p.Type != oldType || p.Note != oldNote {
		return map[i18n.Lang]Term{}, nil
	}
	return carried, nil
}

// notTaken refuses a term another published entry holds in this language: a text must come
// out one way (s1). The entry being corrected does not count, nor does this one.
func notTaken(ctx context.Context, q Querier, l i18n.Lang, text string, self int64, replaces *int64) error {
	var other int64
	err := q.QueryRow(ctx, `SELECT e.id FROM glossary_term t JOIN glossary_entry e ON e.id = t.glossary_entry_id
		WHERE e.status = 'published' AND t.language = $1 AND lower(t.text) = lower($2) AND e.id <> $3 AND e.id IS DISTINCT FROM $4 LIMIT 1`,
		string(l), text, self, replaces).Scan(&other)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &DuplicateError{Entry: other}
}

// Checked is what a keeper's check did.
type Checked struct {
	Corrected bool
	Published bool
	Version   int
	Replaced  *int64
	Change    Change
}

// Check is a keeper's check of their own language, with their words if they corrected it. The
// third check publishes the entry (R-GLO-02), all in tx.
func Check(ctx context.Context, tx pgx.Tx, id int64, l i18n.Lang, text string, keeper int64) (Checked, error) {
	var out Checked
	var status, kind string
	var replaces *int64
	err := tx.QueryRow(ctx, `SELECT status, type, replaces_id FROM glossary_entry WHERE id = $1 FOR UPDATE`, id).Scan(&status, &kind, &replaces)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNoSuchEntry
	}
	if err != nil {
		return out, err
	}
	if status != StatusProposed && status != StatusHeld {
		return out, ErrNotWaiting
	}
	if !slices.Contains(i18n.Langs, l) {
		return out, ErrBadLanguage
	}
	var current string
	var checkedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT text, checked_at FROM glossary_term WHERE glossary_entry_id = $1 AND language = $2`, id, string(l)).Scan(&current, &checkedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	final := current
	if strings.TrimSpace(text) != "" {
		if final, err = CleanTerm(text); err != nil {
			return out, err
		}
	}
	switch {
	case final == "":
		return out, ErrNoText
	case checkedAt != nil && final == current:
		return out, ErrAlreadyChecked
	}
	out.Corrected = current != "" && final != current
	if kind == TypeKeep {
		var differs bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM glossary_term WHERE glossary_entry_id = $1 AND language <> $2 AND text <> $3)`, id, string(l), final).Scan(&differs); err != nil {
			return out, err
		}
		if differs {
			return out, ErrKeepDiffers
		}
	}
	if err := notTaken(ctx, tx, l, final, id, replaces); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO glossary_term (glossary_entry_id, language, text, checked_at, checked_by) VALUES ($1, $2, $3, now(), $4)
		ON CONFLICT (glossary_entry_id, language) DO UPDATE SET text = EXCLUDED.text, checked_at = EXCLUDED.checked_at, checked_by = EXCLUDED.checked_by`,
		id, string(l), final, keeper); err != nil {
		return out, err
	}
	var checked int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM glossary_term WHERE glossary_entry_id = $1 AND checked_at IS NOT NULL`, id).Scan(&checked); err != nil {
		return out, err
	}
	if checked < len(i18n.Langs) {
		_, err := tx.Exec(ctx, `UPDATE glossary_entry SET status = 'held' WHERE id = $1`, id)
		return out, err
	}
	return publish(ctx, tx, id, replaces, keeper, out)
}

// publish gives the entry the next version, and replaces the entry it corrects. The numbers
// are taken one at a time, so the version goes up by exactly one each time (R-GLO-05).
func publish(ctx context.Context, tx pgx.Tx, id int64, replaces *int64, by int64, out Checked) (Checked, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(lockVersion)); err != nil {
		return out, err
	}
	// What was free when it was proposed may have been published since.
	now, err := wordsOf(ctx, tx, id)
	if err != nil {
		return out, err
	}
	for l, t := range now {
		if err := notTaken(ctx, tx, l, t, id, replaces); err != nil {
			return out, err
		}
	}
	var version int
	if err := tx.QueryRow(ctx, `SELECT max(number) + 1 FROM glossary_version`).Scan(&version); err != nil {
		return out, err
	}
	summary := "added 1, replaced 0"
	if replaces != nil {
		summary = "added 0, replaced 1"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO glossary_version (number, published_by, summary) VALUES ($1, $2, $3)`, version, by, summary); err != nil {
		return out, err
	}
	if _, err := tx.Exec(ctx, `UPDATE glossary_entry SET status = 'published', published_in = $2 WHERE id = $1`, id, version); err != nil {
		return out, err
	}
	was := termsByLanguage{}
	if replaces != nil {
		if _, err := tx.Exec(ctx, `UPDATE glossary_entry SET status = 'replaced', replaced_in = $2 WHERE id = $1`, *replaces, version); err != nil {
			return out, err
		}
		if was, err = wordsOf(ctx, tx, *replaces); err != nil {
			return out, err
		}
	}
	if out.Change, err = applyChange(ctx, tx, id, version, was, now); err != nil {
		return out, err
	}
	out.Published, out.Version, out.Replaced = true, version, replaces
	return out, nil
}

// Refuse ends a waiting entry, with why, for the proposer to see (R-GLO-04). A keeper refuses
// by declining to check their own language, so a language already checked gives them no say.
func Refuse(ctx context.Context, tx pgx.Tx, id int64, l i18n.Lang, note string) error {
	note, err := CleanNote(note)
	if err != nil {
		return err
	}
	if note == "" {
		return ErrNoOutcomeNote
	}
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM glossary_entry WHERE id = $1 FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSuchEntry
	}
	if err != nil {
		return err
	}
	if status != StatusProposed && status != StatusHeld {
		return ErrNotWaiting
	}
	var checked bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM glossary_term WHERE glossary_entry_id = $1 AND language = $2 AND checked_at IS NOT NULL)`, id, string(l)).Scan(&checked); err != nil {
		return err
	}
	if checked {
		return ErrAlreadyChecked
	}
	_, err = tx.Exec(ctx, `UPDATE glossary_entry SET status = 'refused', outcome_note = $2 WHERE id = $1`, id, note)
	return err
}

const entrySQL = `SELECT e.id, e.type, e.note, e.status, e.replaces_id,
		(SELECT n.id FROM glossary_entry n WHERE n.replaces_id = e.id AND n.status <> 'refused' ORDER BY n.id DESC LIMIT 1),
		e.published_in, e.replaced_in, e.proposed_at, e.proposed_by, COALESCE(p.name, ''), COALESCE(e.outcome_note, '')
	FROM glossary_entry e LEFT JOIN person p ON p.id = e.proposed_by`

func scanEntry(r pgx.Row) (Entry, error) {
	var e Entry
	err := r.Scan(&e.ID, &e.Type, &e.Note, &e.Status, &e.ReplacesID, &e.ReplacedByID, &e.PublishedIn, &e.ReplacedIn, &e.ProposedAt, &e.ProposedByID, &e.ProposedBy, &e.OutcomeNote)
	return e, err
}

// Get is one entry with its terms.
func Get(ctx context.Context, q Querier, id int64) (Entry, error) {
	e, err := scanEntry(q.QueryRow(ctx, entrySQL+` WHERE e.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNoSuchEntry
	}
	if err != nil {
		return e, err
	}
	list := []*Entry{&e}
	return e, fillTerms(ctx, q, list)
}

// fillTerms reads the terms of each entry, every language always there.
func fillTerms(ctx context.Context, q Querier, list []*Entry) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	byID := make(map[int64]*Entry, len(list))
	for i, e := range list {
		ids[i], byID[e.ID] = e.ID, e
		e.Terms = make([]Term, len(i18n.Langs))
		for j, l := range i18n.Langs {
			e.Terms[j].Language = l
		}
	}
	rows, err := q.Query(ctx, `SELECT t.glossary_entry_id, t.language, t.text, t.checked_at, COALESCE(p.name, '')
		FROM glossary_term t LEFT JOIN person p ON p.id = t.checked_by WHERE t.glossary_entry_id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var lang string
		var t Term
		if err := rows.Scan(&id, &lang, &t.Text, &t.CheckedAt, &t.CheckedBy); err != nil {
			return err
		}
		t.Language = i18n.Lang(lang)
		terms := byID[id].Terms
		terms[slices.Index(i18n.Langs, t.Language)] = t
	}
	return rows.Err()
}

// Like is a pattern finding s anywhere, with s's own % and _ taken as written.
func Like(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

// ErrTooManyProposals says this person has sent the most proposals allowed in a day.
var ErrTooManyProposals = errors.New("glossary: too many proposals in a day")

// ProposedToday counts what this person proposed in the last day. An import is not counted.
func ProposedToday(ctx context.Context, q Querier, person int64) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM glossary_entry WHERE proposed_by = $1 AND glossary_import_id IS NULL
		AND proposed_at > now() - interval '1 day'`, person).Scan(&n)
	return n, err
}

// PageLimit is the most entries one page shows (R-NFR-03): finding by text narrows a longer list.
const PageLimit = 100

// collect reads a page of entries with their terms.
func collect(ctx context.Context, q Querier, sql string, args ...any) ([]Entry, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) { return scanEntry(r) })
	if err != nil {
		return nil, err
	}
	ptrs := make([]*Entry, len(list))
	for i := range list {
		ptrs[i] = &list[i]
	}
	return list, fillTerms(ctx, q, ptrs)
}

const findSQL = `($1 = '' OR EXISTS (SELECT 1 FROM glossary_term f WHERE f.glossary_entry_id = e.id AND f.text ILIKE $2))`

// searchArgs is what findSQL takes for this search, normalised (R-NFR-05).
func searchArgs(find string) (string, string) {
	find = translate.Normalise(strings.TrimSpace(find))
	return find, Like(find)
}

// Published is the entries in force, those holding find if it is not empty.
func Published(ctx context.Context, q Querier, find string) ([]Entry, error) {
	f, like := searchArgs(find)
	return collect(ctx, q, entrySQL+` WHERE e.status = 'published' AND `+findSQL+
		` ORDER BY (SELECT t.text FROM glossary_term t WHERE t.glossary_entry_id = e.id AND t.language = 'en') NULLS LAST, e.id LIMIT $3`, f, like, PageLimit)
}

// AtVersion is the entries in force at a version, so a letter can be checked against the
// glossary it was written with (R-GLO-05).
func AtVersion(ctx context.Context, q Querier, version int, find string) ([]Entry, error) {
	f, like := searchArgs(find)
	return collect(ctx, q, entrySQL+` WHERE e.published_in <= $3 AND (e.replaced_in IS NULL OR e.replaced_in > $3) AND `+findSQL+
		` ORDER BY (SELECT t.text FROM glossary_term t WHERE t.glossary_entry_id = e.id AND t.language = 'en') NULLS LAST, e.id LIMIT $4`, f, like, version, PageLimit)
}

// Waiting is the entries with a language still to check, the given languages' first; no
// languages is every waiting entry.
func Waiting(ctx context.Context, q Querier, langs []i18n.Lang) ([]Entry, error) {
	names := make([]string, len(langs))
	for i, l := range langs {
		names[i] = string(l)
	}
	if len(names) == 0 {
		return collect(ctx, q, entrySQL+` WHERE e.status IN ('proposed', 'held') ORDER BY e.id LIMIT $1`, PageLimit)
	}
	return collect(ctx, q, entrySQL+` WHERE e.status IN ('proposed', 'held') AND EXISTS (SELECT 1 FROM unnest($1::text[]) l(language)
		WHERE NOT EXISTS (SELECT 1 FROM glossary_term t WHERE t.glossary_entry_id = e.id AND t.language = l.language AND t.checked_at IS NOT NULL))
		ORDER BY e.id LIMIT $2`, names, PageLimit)
}

// Chain is an entry's history: every entry it replaced and replaced it, oldest first (R-GLO-02).
func Chain(ctx context.Context, q Querier, id int64) ([]Entry, error) {
	return collect(ctx, q, `WITH RECURSIVE back(id) AS (
			SELECT id FROM glossary_entry WHERE id = $1
			UNION SELECT g.replaces_id FROM glossary_entry g JOIN back ON g.id = back.id WHERE g.replaces_id IS NOT NULL
		), forth(id) AS (
			SELECT id FROM back
			UNION SELECT g.id FROM glossary_entry g JOIN forth ON g.replaces_id = forth.id
		) `+entrySQL+` WHERE e.id IN (SELECT id FROM forth) ORDER BY e.id LIMIT $2`, id, PageLimit)
}

// Version is one published version.
type Version struct {
	Number      int
	PublishedAt time.Time
	By          string
	Added       int
	Replaced    int
}

// Current is the version now.
func Current(ctx context.Context, q Querier) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT COALESCE(max(number), 0) FROM glossary_version`).Scan(&n)
	return n, err
}

// Versions is every version, newest first.
func Versions(ctx context.Context, q Querier) ([]Version, error) {
	rows, err := q.Query(ctx, `SELECT v.number, v.published_at, COALESCE(p.name, ''),
			(SELECT count(*) FROM glossary_entry e WHERE e.published_in = v.number AND e.replaces_id IS NULL),
			(SELECT count(*) FROM glossary_entry e WHERE e.published_in = v.number AND e.replaces_id IS NOT NULL)
		FROM glossary_version v LEFT JOIN person p ON p.id = v.published_by ORDER BY v.number DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Version, error) {
		var v Version
		err := r.Scan(&v.Number, &v.PublishedAt, &v.By, &v.Added, &v.Replaced)
		return v, err
	})
}

// VersionAt is one version.
func VersionAt(ctx context.Context, q Querier, number int) (Version, error) {
	v := Version{Number: number}
	err := q.QueryRow(ctx, `SELECT v.published_at, COALESCE(p.name, ''),
			(SELECT count(*) FROM glossary_entry e WHERE e.published_in = v.number AND e.replaces_id IS NULL),
			(SELECT count(*) FROM glossary_entry e WHERE e.published_in = v.number AND e.replaces_id IS NOT NULL)
		FROM glossary_version v LEFT JOIN person p ON p.id = v.published_by WHERE v.number = $1`, number).Scan(&v.PublishedAt, &v.By, &v.Added, &v.Replaced)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNoVersion
	}
	return v, err
}
