package sources

import (
	"context"
	"errors"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/jackc/pgx/v5"
)

// The language gate (R-GAT): each translation direction is open or closed for each
// source. A direction opens only on a recorded evaluation of this source's version,
// and a new version starts closed. Grammar has no gate.

// The gate's states.
const (
	GateOpen   = "open"
	GateClosed = "closed"
)

// GateKey names one direction for one source.
type GateKey struct {
	Source, From, To string
}

// Gate is the decision in force for one GateKey.
type Gate struct {
	State         string
	SourceVersion string
	TestSet       string
	Score         float64
	EvaluatedOn   time.Time
}

// Open says the source may be used by machine in this direction now: an open decision
// on the version it runs now (R-GAT-01). No decision is closed.
func (p Policy) Open(s bumblebee.Source, from, to string) bool {
	g, ok := p.Gates[GateKey{s.Name, from, to}]
	return ok && g.State == GateOpen && g.SourceVersion == s.Version
}

// Candidates is the sources a piece of this sensitivity may go to, in order: offering
// the work, on for ROSETTA, open under the gate and allowed for its sensitivity
// (R-PRV-02). A resting source is among them: whether it can be called now is the
// fallback's to decide.
func (p Policy) Candidates(l bumblebee.List, d Direction, sensitivity string) []bumblebee.Source {
	if !MayGo(sensitivity) {
		return nil
	}
	ranked := p.Ranked(l, d)
	if d.Operation == bumblebee.OpGrammar {
		return ranked
	}
	return slices.DeleteFunc(ranked, func(s bumblebee.Source) bool { return !p.Open(s, d.From, d.To) })
}

// OpenSource is one source open for a direction, as systems may read it.
type OpenSource struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	EvaluatedOn time.Time `json:"-"`
}

// DirectionState is what R-GAT-04 lets a system read of one direction.
type DirectionState struct {
	From        string
	To          string
	Open        bool
	Sources     []OpenSource
	EvaluatedOn *time.Time // the latest evaluation it rests on
}

// States is every direction's state.
func States(l bumblebee.List, p Policy) []DirectionState {
	var out []DirectionState
	for _, d := range Directions() {
		st := DirectionState{From: d.From, To: d.To}
		for _, s := range p.Candidates(l, d, Ordinary) {
			g := p.Gates[GateKey{s.Name, d.From, d.To}]
			st.Open = true
			st.Sources = append(st.Sources, OpenSource{Name: s.Name, Version: s.Version, EvaluatedOn: g.EvaluatedOn})
			if st.EvaluatedOn == nil || g.EvaluatedOn.After(*st.EvaluatedOn) {
				at := g.EvaluatedOn
				st.EvaluatedOn = &at
			}
		}
		out = append(out, st)
	}
	return out
}

// Evaluation is what an opening rests on (R-GAT-01).
type Evaluation struct {
	TestSet     string
	Score       float64
	EvaluatedOn time.Time
}

// ErrBadEvaluation says the evaluation is not complete or not possible. It repeats
// nothing typed.
var ErrBadEvaluation = errors.New("sources: an opening needs a test set, a score and a date not ahead of today")

var testSetShape = regexp.MustCompile(`^[\pL\pN][\pL\pN ._:-]{0,99}$`)

// ParseEvaluation checks what the form sent. today is the date in Sri Lanka.
func ParseEvaluation(testSet, score, evaluatedOn string, today time.Time) (Evaluation, error) {
	testSet = strings.TrimSpace(testSet)
	if !testSetShape.MatchString(testSet) {
		return Evaluation{}, ErrBadEvaluation
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(score), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return Evaluation{}, ErrBadEvaluation
	}
	on, err := time.Parse(time.DateOnly, strings.TrimSpace(evaluatedOn))
	if err != nil || on.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return Evaluation{}, ErrBadEvaluation
	}
	return Evaluation{TestSet: testSet, Score: n, EvaluatedOn: on}, nil
}

// loadGates adds the decisions in force to p.
func loadGates(ctx context.Context, q Querier, p *Policy) error {
	rows, err := q.Query(ctx, `SELECT s.name, g.from_language, g.to_language, g.state, g.source_version, COALESCE(g.test_set_version, ''),
		COALESCE(g.score, 0)::float8, COALESCE(g.evaluated_on, '0001-01-01'::date)
		FROM gate_decision g JOIN source s ON s.id = g.source_id WHERE g.effective_to IS NULL`)
	if err != nil {
		return err
	}
	type row struct {
		k GateKey
		g Gate
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		err := r.Scan(&x.k.Source, &x.k.From, &x.k.To, &x.g.State, &x.g.SourceVersion, &x.g.TestSet, &x.g.Score, &x.g.EvaluatedOn)
		return x, err
	})
	if err != nil {
		return err
	}
	p.Gates = make(map[GateKey]Gate, len(list))
	for _, x := range list {
		p.Gates[x.k] = x.g
	}
	return nil
}

// decide ends the decision in force, if any, and returns the version the source runs
// now. It holds the source's row, so two decisions cannot cross.
func decide(ctx context.Context, tx pgx.Tx, name, from, to string) (id int64, version string, err error) {
	err = tx.QueryRow(ctx, `SELECT id, version FROM source WHERE name = $1 FOR UPDATE`, name).Scan(&id, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", ErrUnknownSource
	}
	if err != nil {
		return 0, "", err
	}
	_, err = tx.Exec(ctx, `UPDATE gate_decision SET effective_to = now() WHERE source_id = $1 AND from_language = $2 AND to_language = $3 AND effective_to IS NULL`, id, from, to)
	return id, version, err
}

// OpenGate opens a direction for a source, on this evaluation of the version it runs
// now, and reports that version. The old decision stays as history.
func OpenGate(ctx context.Context, tx pgx.Tx, name string, d Direction, e Evaluation, by int64) (string, error) {
	id, version, err := decide(ctx, tx, name, d.From, d.To)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO gate_decision (source_id, from_language, to_language, source_version, state, test_set_version, score, evaluated_on, decided_by)
		VALUES ($1, $2, $3, $4, 'open', $5, $6, $7, $8)`, id, d.From, d.To, version, e.TestSet, e.Score, e.EvaluatedOn, by)
	return version, err
}

// CloseGate closes a direction for a source, and reports that version.
func CloseGate(ctx context.Context, tx pgx.Tx, name string, d Direction, by int64) (string, error) {
	id, version, err := decide(ctx, tx, name, d.From, d.To)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO gate_decision (source_id, from_language, to_language, source_version, state, decided_by)
		VALUES ($1, $2, $3, $4, 'closed', $5)`, id, d.From, d.To, version, by)
	return version, err
}

// GateRow is one decision in the history.
type GateRow struct {
	Source        string
	From, To      string
	SourceVersion string
	State         string
	TestSet       string
	Score         *float64
	EvaluatedOn   *time.Time
	Since         time.Time
	Until         *time.Time
	By            string
}

// GateHistory is the latest decisions (R-ADM-05).
func GateHistory(ctx context.Context, q Querier) ([]GateRow, error) {
	rows, err := q.Query(ctx, `SELECT s.name, g.from_language, g.to_language, g.source_version, g.state, COALESCE(g.test_set_version, ''),
		g.score::float8, g.evaluated_on, g.effective_from, g.effective_to, p.name
		FROM gate_decision g JOIN source s ON s.id = g.source_id JOIN person p ON p.id = g.decided_by ORDER BY g.id DESC LIMIT $1`, HistoryLimit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (GateRow, error) {
		var x GateRow
		err := r.Scan(&x.Source, &x.From, &x.To, &x.SourceVersion, &x.State, &x.TestSet, &x.Score, &x.EvaluatedOn, &x.Since, &x.Until, &x.By)
		return x, err
	})
}
