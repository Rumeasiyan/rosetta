package sources

import (
	"context"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
)

// GrammarProvider is who a grammar call goes to.
type GrammarProvider interface {
	Grammar(ctx context.Context, q bumblebee.GrammarRequest) (bumblebee.GrammarAnswer, error)
}

// GrammarPiece is one masked piece of text to check. Text never goes in an error or a log.
type GrammarPiece struct {
	Ref  string // the request reference, passed as request_id (R-REC-01)
	Text string
}

// GrammarResult is a piece checked.
type GrammarResult struct {
	Suggestions []bumblebee.Suggestion
	Source      string
	Version     string
	Fallback    bool // a source earlier in the order was resting or failed
}

// Grammar sends the piece to the first grammar source that can take it, in the order of their
// English benchmark score unless the administrator set another (R-GRM-06), with the same
// fallback as translation (R-PRV-03). Grammar has no gate, and its text is taken as personal: it
// may go to a source once masked, and nothing of it stays (R-MSK-01, R-GRM-05). Its error is
// as attempt's. An empty list is an answer, not a failure.
func (f *Fallback) Grammar(ctx context.Context, p GrammarPiece) (GrammarResult, error) {
	if f.Grammars == nil {
		return GrammarResult{}, &Exhausted{}
	}
	var found []bumblebee.Suggestion
	a, err := f.attempt(ctx, Grammar, Personal, p.Ref, p.Text, func(ctx context.Context, s bumblebee.Source) (string, error) {
		ans, err := f.Grammars.Grammar(ctx, bumblebee.GrammarRequest{RequestID: p.Ref, Source: s.Name, Text: p.Text})
		if err != nil {
			return "", err
		}
		found = ans.Suggestions
		return ans.Version, nil
	})
	if err != nil {
		return GrammarResult{}, err
	}
	return GrammarResult{Suggestions: found, Source: a.source, Version: a.version, Fallback: a.fallback}, nil
}
