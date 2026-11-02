package sources

import (
	"context"
	"errors"
	"unicode/utf8"

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

// Grammar sends the piece to the first grammar source in order that can be called. Grammar
// has no gate, and its text is taken as personal: it may go to a source once masked, and
// nothing of it stays (R-MSK-01, R-GRM-05). Its error is as Translate's.
func (f *Fallback) Grammar(ctx context.Context, p GrammarPiece) (GrammarResult, error) {
	snap := f.Book.Snapshot(ctx)
	if !snap.Fresh || f.Grammars == nil {
		return GrammarResult{}, &Exhausted{}
	}
	now := f.now()
	for _, s := range f.Policy(ctx).Candidates(snap.List, Grammar, Personal) {
		if !s.Usable(now) {
			continue
		}
		if left, limited := timeLeft(ctx, now); limited && left < leastTime {
			return GrammarResult{}, ErrTooLate
		}
		ans, err := f.Grammars.Grammar(ctx, bumblebee.GrammarRequest{RequestID: p.Ref, Source: s.Name, Text: p.Text})
		var failed *bumblebee.Failure
		switch {
		case err == nil:
			version := ans.Version
			if version == "" {
				version = s.Version
			}
			return GrammarResult{Suggestions: ans.Suggestions, Source: s.Name, Version: version}, nil
		case !errors.As(err, &failed):
			return GrammarResult{}, err
		case failed.Reason == bumblebee.ReasonTooLong:
			f.Book.Refresh(ctx)
			if limit := f.Book.MaxText(); utf8.RuneCountInString(p.Text) > limit {
				return GrammarResult{}, &TooLong{Limit: limit}
			}
		}
		break
	}
	return GrammarResult{}, &Exhausted{}
}
