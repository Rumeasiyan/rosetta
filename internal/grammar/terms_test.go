package grammar

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/terms"
)

func set(words ...string) func(context.Context) (*terms.Set, error) {
	list := make([]terms.Term, len(words))
	for i, w := range words {
		list[i] = terms.Term{Source: w, Target: w}
	}
	s := terms.New(list)
	return func(context.Context) (*terms.Set, error) { return s, nil }
}

func request2(text string) Request { return Request{Ref: "r-1", Language: "en-GB", Text: text} }

// R-GRM-04: a suggestion on a term goes, one beside it stays, and nothing moves.
func TestASuggestionOnATermIsDropped(t *testing.T) {
	f := &fake{}
	s := service(f)
	s.Glossary = set("Recieve Office")
	text := "We recieve from the recieve office and recieve more."
	a, fail := s.Check(context.Background(), request2(text))
	if fail != nil {
		t.Fatal(fail)
	}
	if len(a.Suggestions) != 2 {
		t.Fatalf("%+v", a.Suggestions)
	}
	first, last := strings.Index(text, "recieve"), strings.LastIndex(text, "recieve")
	if a.Suggestions[0].Offset != first || a.Suggestions[1].Offset != last {
		t.Fatalf("%+v", a.Suggestions)
	}
}

// Any overlap counts: over the term, inside it, or running across its edge. Touching is not overlap.
func TestWhatTouchesATerm(t *testing.T) {
	text := "One Good Term two"
	at := strings.Index(text, "Good Term")
	for name, c := range map[string]struct {
		start, length int
		kept          bool
	}{
		"exactly": {at, 9, false}, "inside": {at + 1, 2, false}, "across the start": {at - 2, 4, false}, "across the end": {at + 8, 4, false},
		"around": {0, len(text), false}, "before": {0, at - 1, true}, "up to it": {at - 1, 1, true}, "after": {at + 10, 3, true},
	} {
		f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) {
			return []bumblebee.Suggestion{{Start: c.start, Length: c.length, Message: "m", Category: "style"}}, nil
		}}
		s := service(f)
		s.Glossary = set("Good Term")
		a, fail := s.Check(context.Background(), request2(text))
		if fail != nil {
			t.Fatal(fail)
		}
		if (len(a.Suggestions) == 1) != c.kept {
			t.Errorf("%s: %+v", name, a.Suggestions)
		}
	}
}

// Positions stay true with a character of two units before the term and a masked number between: the
// term is found in the text as sent, in the units the editor counts (R-GRM-03).
func TestPositionsStayTrue(t *testing.T) {
	f := &fake{}
	s := service(f)
	s.Glossary = set("recieve office")
	text := "𝒜𝒜 0771234567 recieve office 𝒜 recieve here"
	a, fail := s.Check(context.Background(), request2(text))
	if fail != nil {
		t.Fatal(fail)
	}
	if len(a.Suggestions) != 1 {
		t.Fatalf("%+v", a.Suggestions)
	}
	if at(text, a.Suggestions[0]) != "recieve" || Encode(text).ContextOf(a.Suggestions[0]).Length != 7 {
		t.Fatalf("%+v at %q", a.Suggestions[0], at(text, a.Suggestions[0]))
	}
}

// Held answers keep every suggestion: the glossary may change within the ten minutes. Published, the
// term is honoured on a held answer; replaced, the suggestion is back.
func TestAHeldAnswerFollowsTheGlossaryNow(t *testing.T) {
	f := &fake{}
	s := service(f)
	s.Held = &Held{}
	s.Glossary = set()
	text := "We recieve and recieve office."
	a, fail := s.Check(context.Background(), request2(text))
	if fail != nil || len(a.Suggestions) != 2 {
		t.Fatalf("%+v %+v", fail, a)
	}
	calls := len(f.sent)
	s.Glossary = set("recieve office")
	if a, _ := s.Check(context.Background(), request2(text)); len(a.Suggestions) != 1 || len(f.sent) != calls {
		t.Fatalf("%+v %d", a, len(f.sent))
	}
	s.Glossary = set()
	if a, _ := s.Check(context.Background(), request2(text)); len(a.Suggestions) != 2 || len(f.sent) != calls {
		t.Fatalf("%+v %d", a, len(f.sent))
	}
}

// With nothing to drop an answer is as it was, and an empty list stays a list.
func TestAnAnswerWithNoSuggestionsLeft(t *testing.T) {
	f := &fake{}
	s := service(f)
	s.Glossary = set("recieve")
	a, fail := s.Check(context.Background(), request2("We recieve."))
	if fail != nil || a.Suggestions == nil || len(a.Suggestions) != 0 {
		t.Fatalf("%+v %+v", fail, a)
	}
	if s.Glossary = nil; len(f.sent) != 1 {
		t.Fatal(f.sent)
	}
	if a, _ := s.Check(context.Background(), request2("We recieve.")); len(a.Suggestions) != 1 {
		t.Fatalf("no glossary at all: %+v", a)
	}
}

// R-GRM-04 cannot be honoured with no glossary to read, so nothing is answered and nothing goes out;
// no text in the log.
func TestAGlossaryThatCannotBeRead(t *testing.T) {
	f := &fake{}
	s := service(f)
	s.Glossary = func(context.Context) (*terms.Set, error) { return nil, errors.New(marker) }
	if _, fail := s.Check(context.Background(), request2("We recieve "+marker+".")); fail == nil || fail.Reason != reasons.NoProvider {
		t.Fatalf("%+v", fail)
	}
	if len(f.sent) != 0 {
		t.Fatal(f.sent)
	}
}
