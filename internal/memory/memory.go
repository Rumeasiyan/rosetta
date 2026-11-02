// Package memory is the shared memory of translations a person approved in a calling system (R-MEM).
// It takes ordinary text only (R-MEM-03). Each approved passage is split into pieces as a translation
// is (R-TRN-04), and a newer approval of a source piece replaces the older one, whose words then go
// (R-NFR-12: memory is kept until replaced).
//
// What is kept is the text a calling system sent, so it reaches no log, record, error or the outbox
// (AGENTS C4): a failure is a reason and a position. Numbers masking recognises are never kept: a
// piece holds the stand-in, as the cache does, and an identity card number refuses the request.
package memory

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/audit"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/translate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// lockMemory keeps two approvals of one piece from crossing, as one live row is allowed for each.
const lockMemory = 0x52535403

// sentenceOnly is a size no sentence reaches: pieces here are whole sentences, as a word break
// depends on a source's limit and would fall in different places in two languages.
const sentenceOnly = 1 << 30

// maxPost is the longest approver's post kept, in characters.
const maxPost = 200

// The states of an entry (Annex A).
const (
	StateServed   = "served"
	StateRecheck  = "recheck"
	StateRemoved  = "removed"
	StateReplaced = "replaced"
)

// Passage is one source passage with its approved translation.
type Passage struct {
	Source   string
	Approved string
}

// AddRequest is approved text from a calling system (R-MEM-01).
type AddRequest struct {
	Ref          string
	CallerID     int64
	System       string // TVA's reference, for the audit entry
	From, To     string
	Sensitivity  string
	ApproverPost string
	ApprovedOn   string // a date, or empty for today in Sri Lanka
	Passages     []Passage
	Visitor      string
}

// Added says what joined. NotKept is each passage whose pieces do not line up.
type Added struct {
	Pieces          int
	NotKept         []int
	GlossaryVersion int
	Mismatch        bool
}

// Service adds approved text.
type Service struct {
	Pool     *pgxpool.Pool
	Settings func(ctx context.Context) settings.Values
	// Kick starts sending the audit entry once it is kept; nil leaves it waiting.
	Kick   func()
	Logger *slog.Logger
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// kept is one pair of pieces, ready to store.
type kept struct {
	source, hash, approved string
}

func validLanguage(l string) bool {
	_, ok := i18n.Parse(l)
	return ok && l == strings.ToLower(strings.TrimSpace(l))
}

// CleanPost is an approver's post as the calling system sent it, normalised. It is text, kept as
// that system's own record (A.1), and may not be a card number by accident.
func CleanPost(post string) (string, bool) {
	post = translate.Normalise(strings.TrimSpace(post))
	if post == "" || utf8.RuneCountInString(post) > maxPost || strings.ContainsFunc(post, unicode.IsControl) || strings.ContainsAny(post, "⟦⟧") || mask.Mask(post).HasCard() {
		return "", false
	}
	return post, true
}

// check refuses what may not be taken: anything but ordinary text is "invalid request" and is never
// kept (R-MEM-03). Limits are those of a translation.
func check(q AddRequest, v settings.Values) *reasons.Failure {
	switch {
	case !validLanguage(q.From), !validLanguage(q.To), q.From == q.To, len(q.Passages) == 0, q.Sensitivity != sources.Ordinary:
		return reasons.New(reasons.Invalid)
	case len(q.Passages) > v.Of(settings.PassagesMax):
		return reasons.New(reasons.TooLong)
	}
	if _, ok := CleanPost(q.ApproverPost); !ok {
		return reasons.New(reasons.Invalid)
	}
	total := 0
	for i, p := range q.Passages {
		if strings.TrimSpace(p.Source) == "" || strings.TrimSpace(p.Approved) == "" {
			return reasons.New(reasons.Invalid).At(i)
		}
		// A stand-in's shape in approved text could not be told from one of ROSETTA's own.
		if strings.ContainsAny(p.Source+p.Approved, "⟦⟧") {
			return reasons.New(reasons.Invalid).At(i)
		}
		n := max(utf8.RuneCountInString(p.Source), utf8.RuneCountInString(p.Approved))
		if n > v.Of(settings.PassageCharsMax) {
			return reasons.New(reasons.TooLong).At(i)
		}
		total += n
	}
	if total > v.Of(settings.RequestCharsMax) {
		return reasons.New(reasons.TooLong)
	}
	return nil
}

// digitsOf is a masked value with its way of being written taken off, so 077 123 4567 and 0771234567
// are one number on the two sides.
func digitsOf(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

// paired writes the approved piece with the source piece's stand-ins, so each restores its own value
// when served. It says false when the two sides do not hold the same numbers.
func paired(source, approved mask.Masked) (string, bool) {
	if len(source.Items) != len(approved.Items) {
		return "", false
	}
	unused := slices.Clone(source.Items)
	var pairs []string
	for _, a := range approved.Items {
		i := slices.IndexFunc(unused, func(s mask.Item) bool { return digitsOf(s.Value) == digitsOf(a.Value) })
		if i < 0 {
			return "", false
		}
		pairs = append(pairs, a.Token, unused[i].Token)
		unused = slices.Delete(unused, i, i+1)
	}
	return strings.NewReplacer(pairs...).Replace(approved.Text), true
}

// pieces lines a passage's two sides up, sentence by sentence (R-MEM-02). It says false when they do
// not line up, since a piece must not be paired with another sentence's words. A piece with no letter
// in it is the same in any language and is not kept.
func pieces(p Passage) ([]kept, bool) {
	source, approved := mask.Mask(p.Source), mask.Mask(p.Approved)
	left, right := translate.Split(source.Text, sentenceOnly), translate.Split(approved.Text, sentenceOnly)
	if len(left) != len(right) {
		return nil, false
	}
	out := make([]kept, 0, len(left))
	for i := range left {
		from, to := source.Slice(left[i].Start, left[i].End), approved.Slice(right[i].Start, right[i].End)
		text, ok := paired(from, to)
		if !ok {
			return nil, false
		}
		if !strings.ContainsFunc(mask.WithoutStandIns(from.Text), unicode.IsLetter) {
			continue
		}
		normal := translate.Normalise(from.Text)
		out = append(out, kept{source: normal, hash: translate.Hash(normal), approved: translate.Normalise(text)})
	}
	return out, true
}

// holdsCard says a text holds an identity card number: personal, whatever it was marked (R-MSK-04).
func holdsCard(p Passage) bool {
	return mask.Mask(p.Source).HasCard() || mask.Mask(p.Approved).HasCard()
}

// Add keeps approved text, with its audit entry in one transaction, or refuses it whole. A passage
// whose pieces do not line up is left out and named in the answer.
func (s *Service) Add(ctx context.Context, q AddRequest) (Added, *reasons.Failure) {
	v := s.Settings(ctx)
	if f := check(q, v); f != nil {
		return Added{}, f
	}
	out := Added{NotKept: []int{}}
	for i, p := range q.Passages {
		if holdsCard(p) {
			s.log().Info("text offered to the memory held an identity card number", "request_ref", q.Ref)
			out.Mismatch = true
			return out, reasons.New(reasons.Invalid).At(i)
		}
	}
	var all []kept
	for i, p := range q.Passages {
		list, ok := pieces(p)
		if !ok {
			out.NotKept = append(out.NotKept, i)
			continue
		}
		all = append(all, list...)
	}
	post, _ := CleanPost(q.ApproverPost)
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var today time.Time
		if err := tx.QueryRow(ctx, `SELECT (now() AT TIME ZONE 'Asia/Colombo')::date`).Scan(&today); err != nil {
			return err
		}
		on, err := approvedOn(q.ApprovedOn, today)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(number), 0) FROM glossary_version`).Scan(&out.GlossaryVersion); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(lockMemory)); err != nil {
			return err
		}
		for _, k := range all {
			if err := keep(ctx, tx, q, k, post, on, out.GlossaryVersion); err != nil {
				return err
			}
		}
		out.Pieces = len(all)
		return audit.Record(ctx, tx, audit.Event{Actor: audit.Actor{Subject: q.System, Post: audit.PostSystem}, Action: audit.ActionCreation, ObjectType: "memory", ObjectRef: q.Ref,
			Result: audit.ResultDone, Visitor: q.Visitor, Details: map[string]any{"from": q.From, "to": q.To, "passages": len(q.Passages), "pieces": out.Pieces, "not_kept": len(out.NotKept)}}, time.Now())
	})
	if errors.Is(err, errBadDate) {
		return Added{}, reasons.New(reasons.Invalid)
	}
	if err != nil {
		s.log().Error("approved text was not kept", "request_ref", q.Ref)
		return Added{}, reasons.New(reasons.NoProvider)
	}
	if s.Kick != nil {
		s.Kick()
	}
	return out, nil
}

var errBadDate = errors.New("memory: not a date from the past or today")

func approvedOn(raw string, today time.Time) (time.Time, error) {
	if raw == "" {
		return today, nil
	}
	d, err := time.Parse(time.DateOnly, raw)
	if err != nil || d.After(today) {
		return time.Time{}, errBadDate
	}
	return d, nil
}

// keep adds one piece, replacing the piece's older approval and taking its words away (R-MEM-02,
// R-NFR-12). The row stays, with its hash and who and when.
func keep(ctx context.Context, tx pgx.Tx, q AddRequest, k kept, post string, on time.Time, version int) error {
	var old *int64
	err := tx.QueryRow(ctx, `UPDATE memory_entry SET state = 'replaced', source_text = '', approved_text = ''
		WHERE source_hash = $1 AND from_language = $2 AND to_language = $3 AND state IN ('served', 'recheck') RETURNING id`, k.hash, q.From, q.To).Scan(&old)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `WITH added AS (
			INSERT INTO memory_entry (caller_id, glossary_version, from_language, to_language, source_text, source_hash, approved_text, approver_post, approved_on)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id
		) UPDATE memory_entry m SET replaced_by_id = added.id FROM added WHERE m.id = $10`,
		q.CallerID, version, q.From, q.To, k.source, k.hash, k.approved, post, on, old)
	return err
}
