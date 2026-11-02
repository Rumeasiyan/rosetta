// Package usage keeps ROSETTA's own count of what it was asked to do (R-REC-01; Annex A:
// usage_record, daily_total), and adds it up for the screens (R-REC-03) and each night
// (R-REC-04). Usage stays here: it never goes to PENSIEVE, one by one or in total (owners
// register). A record holds no text: languages, counts, names of sources and a reason.
package usage

import (
	"context"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The operations (Annex A), and read for a request that does no language work: the gate's state,
// the list of languages or an address that is not there.
const (
	OpTranslate = "translate"
	OpGrammar   = "grammar"
	OpMemoryAdd = "memory add"
	OpTest      = "test"
	OpRead      = "read"
)

// Operations is every operation, in the screens' order.
var Operations = []string{OpTranslate, OpGrammar, OpMemoryAdd, OpTest, OpRead}

// Answered is the outcome of a request that was answered; any other is a reason (R-ERR-01).
const Answered = "answered"

// Querier is what these run through: a pool or a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Pieces counts pieces by where they came from.
type Pieces struct{ Glossary, Memory, Cache, Source int }

// Record is one request, without its text (R-REC-01). Nothing here may hold what a caller
// wrote: From, To and Sensitivity are kept only when they are words from the fixed lists.
type Record struct {
	Ref         string
	CallerID    int64
	Officer     string // the account signed in, for a browser's check or a test from the screens
	Operation   string
	Sensitivity string
	From, To    string
	Passages    int
	Characters  int
	Pieces      Pieces
	Sources     []string
	Fallback    bool
	Mismatch    bool
	DurationMS  int64
	Outcome     string
}

func language(s string) *string {
	if l, ok := i18n.Parse(s); ok && string(l) == s {
		return &s
	}
	return nil
}

func sensitivity(s string) *string {
	switch s {
	case "ordinary", "personal", "confidential":
		return &s
	}
	return nil
}

func officer(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Keep writes one record. The person is found by the account, if they ever signed in here.
func Keep(ctx context.Context, q Querier, r Record) error {
	sources := r.Sources
	if sources == nil {
		sources = []string{}
	}
	_, err := q.Exec(ctx, `INSERT INTO usage_record (request_ref, caller_id, officer_account, person_id, operation, sensitivity, from_language, to_language,
			passages, characters, pieces_glossary, pieces_memory, pieces_cache, pieces_source, sources_used, fallback_used, mismatch, duration_ms, outcome)
		VALUES ($1, $2, $3::text, (SELECT id FROM person WHERE mjolnir_subject = $3::text), $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
		r.Ref, r.CallerID, officer(r.Officer), r.Operation, sensitivity(r.Sensitivity), language(r.From), language(r.To),
		r.Passages, r.Characters, r.Pieces.Glossary, r.Pieces.Memory, r.Pieces.Cache, r.Pieces.Source, sources, r.Fallback, r.Mismatch, r.DurationMS, r.Outcome)
	return err
}
