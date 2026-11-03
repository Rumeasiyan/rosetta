package sources

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
)

var grammarOffer = bumblebee.Offer{Operation: "grammar", From: "en"}

func graded(name string, score float64) bumblebee.Source {
	s := offering(name, grammarOffer)
	s.Scores = map[string]float64{"en": score}
	return s
}

// grammarScript answers each source's calls in turn; the last answer repeats.
type grammarScript struct {
	answers map[string][]func() (bumblebee.GrammarAnswer, error)
	calls   []bumblebee.GrammarRequest
}

func (s *grammarScript) Grammar(_ context.Context, q bumblebee.GrammarRequest) (bumblebee.GrammarAnswer, error) {
	s.calls = append(s.calls, q)
	list := s.answers[q.Source]
	if len(list) == 0 {
		return bumblebee.GrammarAnswer{Suggestions: []bumblebee.Suggestion{{Start: 0, Length: 1, Message: q.Source}}, Source: q.Source}, nil
	}
	next := list[0]
	if len(list) > 1 {
		s.answers[q.Source] = list[1:]
	}
	return next()
}

func (s *grammarScript) called() []string {
	var out []string
	for _, c := range s.calls {
		out = append(out, c.Source)
	}
	return out
}

func grammarFails(reason string, at *time.Time) func() (bumblebee.GrammarAnswer, error) {
	return func() (bumblebee.GrammarAnswer, error) {
		return bumblebee.GrammarAnswer{}, &bumblebee.Failure{Reason: reason, RetryAt: at}
	}
}

type grammarBench struct {
	now      time.Time
	provider *grammarScript
	f        *Fallback
	policy   Policy
	list     bumblebee.List
	slept    []time.Duration
	logs     *bytes.Buffer
}

// newGrammarBench lists best, middle and worst, written out of order, with a fixed clock.
func newGrammarBench(t *testing.T) *grammarBench {
	t.Helper()
	b := &grammarBench{now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), provider: &grammarScript{answers: map[string][]func() (bumblebee.GrammarAnswer, error){}}, logs: &bytes.Buffer{}}
	b.list = bumblebee.List{MaxTextChars: 1000, Sources: []bumblebee.Source{graded("worst", -10), graded("best", 80), offering("translator", enTa), graded("middle", 40)}}
	now := func() time.Time { return b.now }
	book := &Book{Now: now, MaxAge: age(time.Minute), Read: func(context.Context) (bumblebee.List, error) { return b.list, nil }}
	if err := book.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.f = &Fallback{Book: book, Grammars: b.provider, Now: now, Logger: slog.New(slog.NewJSONHandler(b.logs, nil)),
		Policy: func(context.Context) Policy { return b.policy },
		Sleep: func(_ context.Context, d time.Duration) error {
			b.slept = append(b.slept, d)
			b.now = b.now.Add(d)
			return nil
		}}
	return b
}

func (b *grammarBench) send(ctx context.Context) (GrammarResult, error) {
	return b.f.Grammar(ctx, GrammarPiece{Ref: "r-1", Text: marker})
}

func TestGrammarGoesByBenchmarkScore(t *testing.T) {
	b := newGrammarBench(t)
	r, err := b.send(context.Background())
	if err != nil || r.Source != "best" || r.Fallback || r.Version != "1" || len(r.Suggestions) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	if got := b.provider.calls[0]; got.RequestID != "r-1" || got.Text != marker {
		t.Fatalf("%+v", got)
	}
	// With every one failing, the order shows, and a translation source is never asked.
	for _, name := range []string{"best", "middle", "worst"} {
		b.provider.answers[name] = []func() (bumblebee.GrammarAnswer, error){grammarFails("unavailable", nil)}
	}
	b.provider.calls = nil
	if _, err := b.send(context.Background()); err == nil {
		t.Fatal("no failure")
	}
	if got := b.provider.called(); !slices.Equal(got, []string{"best", "middle", "worst"}) {
		t.Fatalf("%v", got)
	}
}

// The administrator's order comes first, and the rest follow by score (R-GRM-06).
func TestTheAdministratorsOrderWins(t *testing.T) {
	b := newGrammarBench(t)
	b.policy = Policy{Orders: map[Direction][]string{Grammar: {"worst"}}}
	for _, name := range []string{"worst", "best", "middle"} {
		b.provider.answers[name] = []func() (bumblebee.GrammarAnswer, error){grammarFails("unavailable", nil)}
	}
	b.send(context.Background())
	if got := b.provider.called(); !slices.Equal(got, []string{"worst", "best", "middle"}) {
		t.Fatalf("%v", got)
	}
	b = newGrammarBench(t)
	b.policy = Policy{Off: map[string]bool{"best": true}}
	r, err := b.send(context.Background())
	if err != nil || r.Source != "middle" || r.Fallback {
		t.Fatalf("a source turned off is no fallback: %+v %v", r, err)
	}
}

func TestEachGrammarFailureGoesToTheNext(t *testing.T) {
	for _, reason := range []string{"blocked", "changed", "timeout", "unsupported", "source-off", "unavailable", "busy", "too-long"} {
		b := newGrammarBench(t)
		b.provider.answers["best"] = []func() (bumblebee.GrammarAnswer, error){grammarFails(reason, nil)}
		r, err := b.send(context.Background())
		if err != nil || r.Source != "middle" || !r.Fallback {
			t.Errorf("%s: %+v %v", reason, r, err)
		}
	}
}

func TestEveryGrammarSourceFailing(t *testing.T) {
	b := newGrammarBench(t)
	soon, later := b.now.Add(time.Minute), b.now.Add(time.Hour)
	b.provider.answers["best"] = []func() (bumblebee.GrammarAnswer, error){grammarFails("blocked", &later)}
	b.provider.answers["middle"] = []func() (bumblebee.GrammarAnswer, error){grammarFails("changed", &soon)}
	b.provider.answers["worst"] = []func() (bumblebee.GrammarAnswer, error){grammarFails("unavailable", nil)}
	_, err := b.send(context.Background())
	var ex *Exhausted
	if !errors.As(err, &ex) || !ex.RetryAt.Equal(soon) {
		t.Fatalf("%v", err)
	}
	if strings.Contains(err.Error(), marker) || strings.Contains(b.logs.String(), marker) {
		t.Fatal("the text escaped")
	}
	// Each is left alone until its time, and nothing is sent.
	b.provider.calls = nil
	if _, err := b.send(context.Background()); err == nil {
		t.Fatal("no failure")
	}
	if got := b.provider.called(); !slices.Equal(got, []string{"worst"}) {
		t.Fatalf("%v", got)
	}
}

func TestARestingGrammarSourceIsSkipped(t *testing.T) {
	b := newGrammarBench(t)
	until := b.now.Add(time.Hour)
	b.list.Sources[1].State, b.list.Sources[1].RestUntil = bumblebee.StateRestingBlock, &until
	b.f.Book.Refresh(context.Background())
	r, err := b.send(context.Background())
	if err != nil || r.Source != "middle" || !r.Fallback {
		t.Fatalf("%+v %v", r, err)
	}
	if got := b.provider.called(); !slices.Equal(got, []string{"middle"}) {
		t.Fatalf("%v", got)
	}
}

func TestBusyGrammarWaitsAndTriesOnceMore(t *testing.T) {
	b := newGrammarBench(t)
	at := b.now.Add(5 * time.Second)
	b.provider.answers["best"] = []func() (bumblebee.GrammarAnswer, error){grammarFails("busy", &at), func() (bumblebee.GrammarAnswer, error) {
		return bumblebee.GrammarAnswer{Suggestions: []bumblebee.Suggestion{}, Source: "best"}, nil
	}}
	r, err := b.send(context.Background())
	if err != nil || r.Source != "best" || r.Fallback || len(b.slept) != 1 || b.slept[0] != 5*time.Second {
		t.Fatalf("%+v %v %v", r, err, b.slept)
	}
}

// An empty list is the source's answer: nobody else is asked.
func TestNoSuggestionsIsAnAnswer(t *testing.T) {
	b := newGrammarBench(t)
	b.provider.answers["best"] = []func() (bumblebee.GrammarAnswer, error){func() (bumblebee.GrammarAnswer, error) {
		return bumblebee.GrammarAnswer{Suggestions: []bumblebee.Suggestion{}, Source: "best"}, nil
	}}
	r, err := b.send(context.Background())
	if err != nil || r.Source != "best" || len(r.Suggestions) != 0 || len(b.provider.calls) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestTooLongGrammarReadsTheListAndSplitsAgain(t *testing.T) {
	b := newGrammarBench(t)
	b.provider.answers["best"] = []func() (bumblebee.GrammarAnswer, error){func() (bumblebee.GrammarAnswer, error) {
		b.list.MaxTextChars = 3
		return bumblebee.GrammarAnswer{}, &bumblebee.Failure{Reason: bumblebee.ReasonTooLong}
	}}
	_, err := b.send(context.Background())
	var tl *TooLong
	if !errors.As(err, &tl) || tl.Limit != 3 {
		t.Fatalf("%v", err)
	}
	if len(b.provider.calls) != 1 {
		t.Fatalf("%v", b.provider.called())
	}
}

func TestAStaleListSendsNoGrammar(t *testing.T) {
	b := newGrammarBench(t)
	b.now = b.now.Add(2 * time.Minute)
	_, err := b.send(context.Background())
	var ex *Exhausted
	if !errors.As(err, &ex) || len(b.provider.calls) != 0 {
		t.Fatalf("%v %v", err, b.provider.called())
	}
}

func TestGrammarStaysInsideItsTime(t *testing.T) {
	b := newGrammarBench(t)
	ctx, cancel := context.WithDeadline(context.Background(), b.now.Add(time.Second))
	defer cancel()
	if _, err := b.send(ctx); !errors.Is(err, ErrTooLate) || len(b.provider.calls) != 0 {
		t.Fatalf("%v", err)
	}
}

// A hold set by one operation holds the source for the other (R-PRV-05).
func TestAHoldCoversBothOperations(t *testing.T) {
	b := newGrammarBench(t)
	at := b.now.Add(time.Hour)
	b.f.hold("best", &at)
	r, err := b.send(context.Background())
	if err != nil || r.Source != "middle" || !r.Fallback {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestNoGrammarProviderIsExhausted(t *testing.T) {
	b := newGrammarBench(t)
	b.f.Grammars = nil
	var ex *Exhausted
	if _, err := b.send(context.Background()); !errors.As(err, &ex) {
		t.Fatal(err)
	}
}
