package translate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/sources"
)

// memoryCache is the cache's rule without the table: the same piece answers again.
type memoryCache struct {
	kept map[string]Keep
	err  error
}

func (m *memoryCache) Answer(_ context.Context, l Lookup) (Hit, bool, error) {
	k, ok := m.kept[l.Normal]
	if !ok {
		return Hit{}, false, nil
	}
	return Hit{Text: k.Translation, Origin: OriginCache, Source: k.Source, Version: k.Version}, true, nil
}

func (m *memoryCache) Keep(_ context.Context, k Keep) error {
	if m.err != nil {
		return m.err
	}
	if m.kept == nil {
		m.kept = map[string]Keep{}
	}
	m.kept[k.Normal] = k
	return nil
}

func TestTheSameRequestGivesTheSameAnswerWithNoSecondCall(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	c := &memoryCache{}
	s.Cache, s.Keeper = c, c
	q := request("  One.\n\nCall 0771234567 now.  ")
	first, fail := s.Translate(context.Background(), q)
	if fail != nil {
		t.Fatal(fail)
	}
	calls := len(f.sent)
	if calls != 2 || first.Passages[0].Pieces[0].Origin != OriginSource {
		t.Fatalf("%d %+v", calls, first)
	}
	second, fail := s.Translate(context.Background(), q)
	if fail != nil {
		t.Fatal(fail)
	}
	if second.Passages[0].Text != first.Passages[0].Text || len(f.sent) != calls {
		t.Fatalf("%q, %d calls", second.Passages[0].Text, len(f.sent))
	}
	if p := second.Passages[0].Pieces[0]; p.Origin != OriginCache || p.Source != "a" || p.Version != "1" {
		t.Fatalf("%+v", p)
	}
	// What is kept holds the stand-in, never the number; another number uses the same.
	for _, k := range c.kept {
		if strings.Contains(k.Normal+k.Translation, "0771234567") {
			t.Fatalf("%+v", k)
		}
	}
	other, fail := s.Translate(context.Background(), request("Call 0719999999 now."))
	if fail != nil || other.Passages[0].Text != "[ta] Call 0719999999 now." || len(f.sent) != calls {
		t.Fatalf("%+v %d", other, len(f.sent))
	}
}

func TestOnlyOrdinaryTextIsKept(t *testing.T) {
	for name, q := range map[string]Request{
		"personal":       requestOf(sources.Personal, "A person's letter."),
		"ordinary, card": requestOf(sources.Ordinary, "Card 123456789V here."),
	} {
		f := &fakeSources{}
		s, _ := newService(f)
		c := &memoryCache{}
		s.Cache, s.Keeper = c, c
		if _, fail := s.Translate(context.Background(), q); fail != nil {
			t.Fatal(fail)
		}
		if len(c.kept) != 0 {
			t.Errorf("%s: %+v", name, c.kept)
		}
	}
}

// What failed masking is never kept: only what came back whole.
func TestALostStandInIsNotKept(t *testing.T) {
	f := &fakeSources{answer: func(sources.Piece) (sources.Result, error) {
		return sources.Result{Text: "lost it", Source: "a", Version: "1"}, nil
	}}
	s, _ := newService(f)
	c := &memoryCache{}
	s.Cache, s.Keeper = c, c
	if _, fail := s.Translate(context.Background(), request("Call 0771234567 now.")); fail == nil {
		t.Fatal("no failure")
	}
	if len(c.kept) != 0 {
		t.Fatalf("%+v", c.kept)
	}
}

// A piece before a later failure stays kept, so sending again does not pay twice.
func TestAPieceBeforeAFailureIsKept(t *testing.T) {
	f := &fakeSources{}
	f.answer = func(p sources.Piece) (sources.Result, error) {
		if strings.Contains(p.Text, "Second") {
			return sources.Result{}, &sources.Exhausted{}
		}
		return sources.Result{Text: "[ta] " + p.Text, Source: "a", Version: "1"}, nil
	}
	s, _ := newService(f)
	c := &memoryCache{}
	s.Cache, s.Keeper = c, c
	if _, fail := s.Translate(context.Background(), request("First.", "Second.")); fail == nil {
		t.Fatal("no failure")
	}
	if _, ok := c.kept["First."]; !ok || len(c.kept) != 1 {
		t.Fatalf("%+v", c.kept)
	}
}

// The cache failing costs a source call, never the answer, and logs no text.
func TestACacheThatFailsChangesNoAnswer(t *testing.T) {
	f := &fakeSources{}
	s, logs := newService(f)
	s.Keeper = &memoryCache{err: errors.New(marker)}
	a, fail := s.Translate(context.Background(), request("An invented "+marker+"."))
	if fail != nil || !strings.HasPrefix(a.Passages[0].Text, "[ta] ") {
		t.Fatalf("%+v %+v", fail, a)
	}
	if strings.Contains(logs.String(), marker) || !strings.Contains(logs.String(), "not kept") {
		t.Fatal(logs)
	}
}

// A piece split again is kept whole, under the numbers it has as a whole, when one source
// made it all.
func TestAPieceSplitAgainIsKeptWhole(t *testing.T) {
	first := true
	f := &fakeSources{}
	f.answer = func(p sources.Piece) (sources.Result, error) {
		if first {
			first = false
			return sources.Result{}, &sources.TooLong{Limit: 14}
		}
		return sources.Result{Text: "[ta] " + p.Text, Source: "a", Version: "1"}, nil
	}
	s, _ := newService(f)
	c := &memoryCache{}
	s.Cache, s.Keeper = c, c
	q := request("Call 0771234567 first then 0719999999 second")
	a, fail := s.Translate(context.Background(), q)
	if fail != nil {
		t.Fatal(fail)
	}
	calls := len(f.sent)
	again, fail := s.Translate(context.Background(), q)
	if fail != nil || again.Passages[0].Text != a.Passages[0].Text || len(f.sent) != calls {
		t.Fatalf("%+v\n%q\n%q %d %d", fail, a.Passages[0].Text, again.Passages[0].Text, calls, len(f.sent))
	}
}

func TestHashSaysNothingOfTheText(t *testing.T) {
	h := Hash(marker)
	if strings.Contains(h, marker) || len(h) != 64 || h == Hash(marker+" ") {
		t.Fatal(h)
	}
}
