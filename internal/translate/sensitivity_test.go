package translate

import (
	"context"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/terms"
)

// spyAnswerer notes what it is asked.
type spyAnswerer struct{ asked []string }

// Terms is what the pipeline asks the glossary: being asked is what is counted.
func (a *spyAnswerer) Terms(_ context.Context, _, _ string) (int, *terms.Set, error) {
	a.asked = append(a.asked, "terms")
	return 0, nil, nil
}

func (a *spyAnswerer) Answer(_ context.Context, l Lookup) (Hit, bool, error) {
	a.asked = append(a.asked, l.Normal)
	return Hit{}, false, nil
}

func requestOf(sensitivity string, passages ...string) Request {
	return Request{Ref: "r-1", From: "en", To: "ta", Sensitivity: sensitivity, Passages: passages}
}

// R-MSK-01: confidential text is refused, and goes nowhere at all.
func TestConfidentialGoesNowhere(t *testing.T) {
	f := &fakeSources{}
	s, logs := newService(f)
	glossary, memory, cache := &spyAnswerer{}, &spyAnswerer{}, &spyAnswerer{}
	s.Glossary, s.Memory, s.Cache = glossary, memory, cache
	a, fail := s.Translate(context.Background(), requestOf(sources.Confidential, marker))
	if fail == nil || fail.Reason != reasons.NoProvincial || a.Passages != nil {
		t.Fatalf("%+v", fail)
	}
	if len(f.sent)+len(glossary.asked)+len(memory.asked)+len(cache.asked) != 0 {
		t.Fatal("it went somewhere")
	}
	if strings.Contains(logs.String(), marker) {
		t.Fatal(logs)
	}
}

// R-TRN-03: there is no default, and no other word.
func TestSensitivityMustBeStated(t *testing.T) {
	s, _ := newService(&fakeSources{})
	for _, bad := range []string{"", "Ordinary", "public", marker} {
		if _, fail := s.Translate(context.Background(), requestOf(bad, "A piece.")); fail == nil || fail.Reason != reasons.Invalid {
			t.Errorf("%q: %+v", bad, fail)
		}
	}
}

// R-MSK-01, R-TRN-08: personal text is masked and goes out, but never near the cache.
func TestPersonalIsMaskedAndNeverNearTheCache(t *testing.T) {
	f := &fakeSources{}
	s, logs := newService(f)
	glossary, memory, cache := &spyAnswerer{}, &spyAnswerer{}, &spyAnswerer{}
	s.Glossary, s.Memory, s.Cache = glossary, memory, cache
	const text = "A. Perera " + marker + " of 0771234567."
	a, fail := s.Translate(context.Background(), requestOf(sources.Personal, text))
	if fail != nil {
		t.Fatal(fail)
	}
	if a.Sensitivity != sources.Personal || a.Mismatch {
		t.Fatalf("%+v", a)
	}
	if len(cache.asked) != 0 {
		t.Fatal("the cache was asked")
	}
	if len(glossary.asked) != 1 || len(memory.asked) != 1 {
		t.Fatal("people's checked work answers whatever the text is")
	}
	if strings.Contains(f.sent[0].Text, "0771234567") || f.sent[0].Sensitivity != sources.Personal {
		t.Fatalf("%+v", f.sent[0])
	}
	if strings.Contains(logs.String(), marker) {
		t.Fatal(logs)
	}
}

// R-MSK-04: ordinary text holding a card number is treated as personal, the whole
// request with it, and the mismatch says so. A phone number alone is no mismatch.
func TestOrdinaryHoldingACardIsTreatedAsPersonal(t *testing.T) {
	for name, c := range map[string]struct {
		passages []string
		mismatch bool
		want     string
	}{
		"old card":  {[]string{"Card 123456789V here."}, true, sources.Personal},
		"new card":  {[]string{"Fine.", "Card 199012345678 here."}, true, sources.Personal},
		"a phone":   {[]string{"Call 0771234567 now."}, false, sources.Ordinary},
		"no number": {[]string{"Nothing here."}, false, sources.Ordinary},
	} {
		f := &fakeSources{}
		s, logs := newService(f)
		cache := &spyAnswerer{}
		s.Cache = cache
		a, fail := s.Translate(context.Background(), requestOf(sources.Ordinary, c.passages...))
		if fail != nil {
			t.Fatal(fail)
		}
		if a.Mismatch != c.mismatch || a.Sensitivity != c.want {
			t.Errorf("%s: %+v", name, a)
		}
		if c.mismatch && len(cache.asked) != 0 {
			t.Errorf("%s: the cache was asked", name)
		}
		if !c.mismatch && len(cache.asked) == 0 {
			t.Errorf("%s: ordinary text looks in the cache", name)
		}
		for _, p := range f.sent {
			if p.Sensitivity != c.want || strings.Contains(p.Text, "123456789V") || strings.Contains(p.Text, "199012345678") {
				t.Errorf("%s: %+v", name, p)
			}
		}
		if strings.Contains(logs.String(), "123456789V") || strings.Contains(logs.String(), "199012345678") {
			t.Errorf("%s: %s", name, logs)
		}
	}
}
