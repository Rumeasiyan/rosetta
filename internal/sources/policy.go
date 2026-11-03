package sources

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
)

// Sensitivities a request states (R-TRN-03).
const (
	Ordinary     = "ordinary"
	Personal     = "personal"
	Confidential = "confidential"
)

// MayGo says text of this sensitivity may go to BUMBLEBEE's sources, which are all
// outside public websites (R-MSK-01, R-NFR-13). Confidential text goes only to
// providers on provincial machines, and there are none yet.
func MayGo(sensitivity string) bool { return sensitivity == Ordinary || sensitivity == Personal }

// Direction is a translation direction, or grammar for one language. As the table
// has it, grammar's To is the same as its From.
type Direction struct {
	Operation string
	From, To  string
}

// Grammar is the one grammar direction there is (R-GRM-02).
var Grammar = Direction{Operation: bumblebee.OpGrammar, From: "en", To: "en"}

// Translate is one translation direction.
func Translate(from, to string) Direction {
	return Direction{Operation: bumblebee.OpTranslate, From: from, To: to}
}

// Directions is all six translation directions, in the screens' order.
func Directions() []Direction {
	var out []Direction
	for _, from := range i18n.Langs {
		for _, to := range i18n.Langs {
			if from != to {
				out = append(out, Translate(string(from), string(to)))
			}
		}
	}
	return out
}

// ErrBadDirection says this is not a direction.
var ErrBadDirection = errors.New("sources: not a direction")

// ParseDirection reads a direction from the address.
func ParseDirection(operation, from, to string) (Direction, error) {
	d := Direction{Operation: operation, From: from, To: to}
	if d == Grammar || (operation == bumblebee.OpTranslate && slices.Contains(Directions(), d)) {
		return d, nil
	}
	return Direction{}, ErrBadDirection
}

// String names the direction in an audit entry and in history.
func (d Direction) String() string {
	if d.Operation == bumblebee.OpGrammar {
		return d.Operation + " " + d.From
	}
	return d.Operation + " " + d.From + "-" + d.To
}

// offeredBy says a source does this work. CALLERS.md's grammar offers have no to_language.
func (d Direction) offeredBy(s bumblebee.Source) bool {
	if d.Operation == bumblebee.OpGrammar {
		return s.Offer(d.Operation, d.From, "")
	}
	return s.Offer(d.Operation, d.From, d.To)
}

// Policy is what the administrator has set (R-PRV-02). The zero Policy has nothing
// set: every source on, in its natural order.
type Policy struct {
	Orders map[Direction][]string // the sources placed, first first
	Off    map[string]bool        // turned off for ROSETTA
	Gates  map[GateKey]Gate       // the decision in force for each direction and source
}

// Ranked is the sources offering this work and on for ROSETTA, in order: the
// administrator's first, then the rest. The rest go by name, so the order never
// depends on how the list happened to be written; for grammar, by English benchmark
// score, best first, unless the administrator sets another order (R-GRM-06).
func (p Policy) Ranked(l bumblebee.List, d Direction) []bumblebee.Source {
	var out []bumblebee.Source
	for _, s := range l.Sources {
		if d.offeredBy(s) && !p.Off[s.Name] {
			out = append(out, s)
		}
	}
	placed := p.Orders[d]
	rank := func(s bumblebee.Source) int {
		if i := slices.Index(placed, s.Name); i >= 0 {
			return i
		}
		return len(placed)
	}
	slices.SortStableFunc(out, func(a, b bumblebee.Source) int {
		if c := cmp.Compare(rank(a), rank(b)); c != 0 {
			return c
		}
		if d.Operation == bumblebee.OpGrammar {
			if c := cmp.Compare(b.Scores[d.From], a.Scores[d.From]); c != 0 {
				return c
			}
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// ErrBadOrder says the numbers given are not an order. It repeats none of them.
var ErrBadOrder = errors.New("sources: the positions are not whole numbers from 1, each used once")

// ParseOrder reads an order from the form: a position for each source that has one.
// A source left blank is not placed. Only the numbers' order counts, so 2 and 5 mean
// first and second.
func ParseOrder(names []string, position func(name string) string) ([]string, error) {
	type placed struct {
		name string
		at   int
	}
	var list []placed
	seen := map[int]bool{}
	for _, name := range names {
		v := strings.TrimSpace(position(name))
		if v == "" {
			continue
		}
		at, err := strconv.Atoi(v)
		if err != nil || at < 1 || seen[at] || strings.HasPrefix(v, "+") {
			return nil, ErrBadOrder
		}
		seen[at] = true
		list = append(list, placed{name, at})
	}
	slices.SortFunc(list, func(a, b placed) int { return cmp.Compare(a.at, b.at) })
	out := make([]string, 0, len(list))
	for _, x := range list {
		out = append(out, x.name)
	}
	return out, nil
}
