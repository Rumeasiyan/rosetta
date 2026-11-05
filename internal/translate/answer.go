package translate

import (
	"context"
	"slices"
	"strings"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/jackc/pgx/v5"
)

// LabelWord and LabelThroughWord are the message keys of the label on what a machine
// produced (R-TRN-07). The second names the source, in place of {source}.
const (
	LabelWord        = "machineLabel"
	LabelThroughWord = "machineLabelThrough"
	PublicLabelWord  = "machineLabelPublic"
	PublicLinkWord   = "machineLabelPublicLink"
)

// Label is the passage's label in the three languages, naming each source that made any
// of it, for the caller to show beside the text.
func (p Passage) Label() map[string]string {
	names := []string{}
	for _, u := range p.Sources() {
		if !slices.Contains(names, u.Name) {
			names = append(names, u.Name)
		}
	}
	out := make(map[string]string, len(i18n.Langs))
	for _, l := range i18n.Langs {
		if len(names) == 0 {
			out[string(l)] = i18n.T(l, LabelWord)
			continue
		}
		out[string(l)] = strings.ReplaceAll(i18n.T(l, LabelThroughWord), "{source}", strings.Join(names, ", "))
	}
	return out
}

// PublicLabel is the same notice for a public page, in the three languages. It names
// ROSETTA, not the outside source, and asks nothing of the reader that an officer does.
func (p Passage) PublicLabel() map[string]string {
	out := make(map[string]string, len(i18n.Langs))
	for _, l := range i18n.Langs {
		out[string(l)] = i18n.T(l, PublicLabelWord)
	}
	return out
}

// PublicLabelLink is, in each language, the words inside the public label that a page may
// make its link for telling the service, so the label stays whole and no caller has to
// find them in a language it cannot read.
func (p Passage) PublicLabelLink() map[string]string {
	out := make(map[string]string, len(i18n.Langs))
	for _, l := range i18n.Langs {
		out[string(l)] = i18n.T(l, PublicLinkWord)
	}
	return out
}

// Machine says a machine produced any of the passage: a source now, or earlier by way
// of the cache (R-TRN-07).
func (p Passage) Machine() bool {
	for _, x := range p.Pieces {
		if x.Origin == OriginSource || x.Origin == OriginCache {
			return true
		}
	}
	return false
}

// CheckedByPerson says the passage is wholly from the glossary or the memory. A passage
// with nothing in it has nobody's work in it either.
func (p Passage) CheckedByPerson() bool { return len(p.Pieces) > 0 && !p.Machine() }

// Fallback says a piece of the passage used fallback (R-PRV-03).
func (p Passage) Fallback() bool {
	for _, x := range p.Pieces {
		if x.Fallback {
			return true
		}
	}
	return false
}

// SourceUsed is a source and its version.
type SourceUsed struct {
	Name    string
	Version string
}

// Sources is each source that made any of the passage, once, in the order first used.
func (p Passage) Sources() []SourceUsed {
	out := []SourceUsed{}
	for _, x := range p.Pieces {
		if x.Source == "" {
			continue
		}
		u := SourceUsed{x.Source, x.Version}
		seen := false
		for _, o := range out {
			seen = seen || o == u
		}
		if !seen {
			out = append(out, u)
		}
	}
	return out
}

// Origins counts the passage's pieces by where they came from, each always there.
func (p Passage) Origins() map[string]int {
	out := map[string]int{OriginGlossary: 0, OriginMemory: 0, OriginCache: 0, OriginSource: 0}
	for _, x := range p.Pieces {
		out[x.Origin]++
	}
	return out
}

// VersionQuerier is what the glossary's version is read through.
type VersionQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GlossaryVersion reads the glossary's version now (R-GLO-05). It is read on every
// request, as every answer must state the one it used.
func GlossaryVersion(q VersionQuerier) func(ctx context.Context) (int, error) {
	return func(ctx context.Context) (int, error) {
		var n int
		err := q.QueryRow(ctx, `SELECT COALESCE(max(number), 0) FROM glossary_version`).Scan(&n)
		return n, err
	}
}
