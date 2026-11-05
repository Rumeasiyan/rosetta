package translate

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/reasons"
)

func TestTheLabelNamesTheSourceInThreeLanguages(t *testing.T) {
	p := Passage{Pieces: []PieceInfo{{Origin: OriginSource, Source: "a", Version: "1"}, {Origin: OriginCache, Source: "b", Version: "2"}, {Origin: OriginSource, Source: "a", Version: "2"}}}
	l := p.Label()
	if l["en"] != "Machine translation through a, b, check before issue" {
		t.Fatal(l["en"])
	}
	for lang, word := range l {
		if !strings.Contains(word, "a, b") || strings.Contains(word, "{source}") {
			t.Fatal(lang, word)
		}
	}
	if plain := (Passage{Pieces: []PieceInfo{{Origin: OriginCache}}}).Label(); plain["en"] != "Machine translation, check before issue" {
		t.Fatal(plain)
	}
	if !regexp.MustCompile(`\p{Sinhala}`).MatchString(l["si"]) || !regexp.MustCompile(`\p{Tamil}`).MatchString(l["ta"]) || len(l) != 3 {
		t.Fatal(l)
	}
}

// The public wording names ROSETTA and never the outside source (R-TRN-07).
func TestThePublicLabelNamesRosettaNotTheSource(t *testing.T) {
	p := Passage{Pieces: []PieceInfo{{Origin: OriginSource, Source: "some-site", Version: "1"}}}
	l := p.PublicLabel()
	if l["en"] != "Machine translated through the provincial translation service ROSETTA. Please read with caution: it may contain mistakes. Please tell us if anything needs changing." {
		t.Fatal(l["en"])
	}
	if len(l) != 3 || !regexp.MustCompile(`\p{Sinhala}`).MatchString(l["si"]) || !regexp.MustCompile(`\p{Tamil}`).MatchString(l["ta"]) {
		t.Fatal(l)
	}
	for lang, word := range l {
		if !strings.Contains(word, "ROSETTA") || strings.Contains(word, "some-site") {
			t.Fatal(lang, word)
		}
	}
	for lang, link := range p.PublicLabelLink() {
		if link == "" || strings.Count(l[lang], link) != 1 {
			t.Fatal(lang, link)
		}
	}
}

// R-TRN-07: machine in whole or part carries the label; wholly from people does not and
// says who checked it; nothing in it says neither.
func TestWhoMadeAPassage(t *testing.T) {
	machine := []PieceInfo{{Origin: OriginSource, Source: "a", Version: "1"}}
	for name, c := range map[string]struct {
		pieces       []PieceInfo
		machine, why bool
	}{
		"a source":         {machine, true, false},
		"the cache":        {[]PieceInfo{{Origin: OriginCache, Source: "a", Version: "1"}}, true, false},
		"in part":          {[]PieceInfo{{Origin: OriginGlossary}, {Origin: OriginSource, Source: "a"}}, true, false},
		"glossary":         {[]PieceInfo{{Origin: OriginGlossary}}, false, true},
		"memory":           {[]PieceInfo{{Origin: OriginMemory}}, false, true},
		"glossary, memory": {[]PieceInfo{{Origin: OriginGlossary}, {Origin: OriginMemory}}, false, true},
		"nothing in it":    {nil, false, false},
	} {
		p := Passage{Pieces: c.pieces}
		if p.Machine() != c.machine || p.CheckedByPerson() != c.why {
			t.Errorf("%s: %v %v", name, p.Machine(), p.CheckedByPerson())
		}
	}
}

func TestSummariesOfAPassage(t *testing.T) {
	p := Passage{Pieces: []PieceInfo{
		{Origin: OriginSource, Source: "a", Version: "1"},
		{Origin: OriginSource, Source: "a", Version: "1"},
		{Origin: OriginCache, Source: "b", Version: "2", Fallback: true},
		{Origin: OriginGlossary},
	}}
	if got := p.Sources(); len(got) != 2 || got[0] != (SourceUsed{"a", "1"}) || got[1] != (SourceUsed{"b", "2"}) {
		t.Fatal(got)
	}
	o := p.Origins()
	if o[OriginSource] != 2 || o[OriginCache] != 1 || o[OriginGlossary] != 1 || o[OriginMemory] != 0 || len(o) != 4 {
		t.Fatal(o)
	}
	if !p.Fallback() || (Passage{}).Fallback() {
		t.Fatal("fallback")
	}
	if s := (Passage{}).Sources(); s == nil || len(s) != 0 {
		t.Fatal("an empty list, not none")
	}
}

func TestTheGlossaryVersionIsRead(t *testing.T) {
	f := &fakeSources{}
	s, logs := newService(f)
	s.Version = func(context.Context) (int, error) { return 7, nil }
	a, fail := s.Translate(context.Background(), request("A piece."))
	if fail != nil || a.GlossaryVersion != 7 || a.From != "en" || a.To != "ta" {
		t.Fatalf("%+v %+v", fail, a)
	}
	// Every answer must state it, so with no way to read it there is no answer.
	s.Version = func(context.Context) (int, error) { return 0, errors.New(marker) }
	f.sent = nil
	if _, fail := s.Translate(context.Background(), request("A piece.")); fail == nil || fail.Reason != reasons.NoProvider || len(f.sent) != 0 {
		t.Fatalf("%+v", fail)
	}
	if containsMarker(logs.String()) {
		t.Fatal(logs)
	}
}

func containsMarker(s string) bool { return regexp.MustCompile(marker).MatchString(s) }
