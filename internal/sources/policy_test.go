package sources

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
)

func offering(name string, offers ...bumblebee.Offer) bumblebee.Source {
	return bumblebee.Source{Name: name, Version: "1", State: "on", Offers: offers}
}

var enTa = bumblebee.Offer{Operation: "translate", From: "en", To: "ta"}

func names(list []bumblebee.Source) []string {
	var out []string
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

func TestRankedFollowsTheOrderAndTheSwitch(t *testing.T) {
	l := bumblebee.List{Sources: []bumblebee.Source{offering("c", enTa), offering("a", enTa), offering("b", enTa), offering("elsewhere", bumblebee.Offer{Operation: "translate", From: "si", To: "ta"})}}
	d := Translate("en", "ta")
	if got := names(Policy{}.Ranked(l, d)); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatal("nothing set goes by name", got)
	}
	p := Policy{Orders: map[Direction][]string{d: {"c", "b"}, Translate("ta", "en"): {"a"}}}
	if got := names(p.Ranked(l, d)); !slices.Equal(got, []string{"c", "b", "a"}) {
		t.Fatal("placed first, the rest after", got)
	}
	p.Off = map[string]bool{"c": true}
	if got := names(p.Ranked(l, d)); !slices.Equal(got, []string{"b", "a"}) {
		t.Fatal("a source turned off is skipped", got)
	}
	// A source placed but gone from the list is simply not there.
	p = Policy{Orders: map[Direction][]string{d: {"gone", "b"}}}
	if got := names(p.Ranked(l, d)); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatal(got)
	}
}

func TestGrammarGoesByBenchmarkUnlessSet(t *testing.T) {
	gr := bumblebee.Offer{Operation: "grammar", From: "en"}
	a, b, c := offering("a", gr), offering("b", gr), offering("c", gr)
	a.Scores, b.Scores, c.Scores = map[string]float64{"en": 10}, map[string]float64{"en": 72}, map[string]float64{"en": -5}
	l := bumblebee.List{Sources: []bumblebee.Source{a, b, c, offering("translates", enTa)}}
	if got := names(Policy{}.Ranked(l, Grammar)); !slices.Equal(got, []string{"b", "a", "c"}) {
		t.Fatal(got)
	}
	p := Policy{Orders: map[Direction][]string{Grammar: {"c"}}}
	if got := names(p.Ranked(l, Grammar)); !slices.Equal(got, []string{"c", "b", "a"}) {
		t.Fatal(got)
	}
}

func TestDirections(t *testing.T) {
	if got := Directions(); len(got) != 6 || got[0].String() != "translate si-ta" {
		t.Fatal(got)
	}
	if Grammar.String() != "grammar en" {
		t.Fatal(Grammar)
	}
	if d, err := ParseDirection("translate", "ta", "si"); err != nil || d != Translate("ta", "si") {
		t.Fatal(d, err)
	}
	if d, err := ParseDirection("grammar", "en", "en"); err != nil || d != Grammar {
		t.Fatal(d, err)
	}
	for _, bad := range [][3]string{{"translate", "en", "en"}, {"translate", "fr", "en"}, {"grammar", "ta", "ta"}, {"grammar", "en", "ta"}, {"guess", "en", "ta"}, {"", "", ""}} {
		if _, err := ParseDirection(bad[0], bad[1], bad[2]); !errors.Is(err, ErrBadDirection) {
			t.Error(bad)
		}
	}
}

func TestMayGo(t *testing.T) {
	for s, want := range map[string]bool{Ordinary: true, Personal: true, Confidential: false, "": false, "secret": false} {
		if MayGo(s) != want {
			t.Errorf("%q", s)
		}
	}
}

func TestParseOrder(t *testing.T) {
	form := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	all := []string{"a", "b", "c"}
	got, err := ParseOrder(all, form(map[string]string{"a": "5", "b": "", "c": " 2 "}))
	if err != nil || !slices.Equal(got, []string{"c", "a"}) {
		t.Fatal(got, err)
	}
	if got, err := ParseOrder(all, form(nil)); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
	for _, bad := range []map[string]string{{"a": "1", "b": "1"}, {"a": "0"}, {"a": "-1"}, {"a": "+1"}, {"a": "one"}, {"a": "1.5"}, {"a": "CALLERTEXTMARKER"}} {
		_, err := ParseOrder(all, form(bad))
		if !errors.Is(err, ErrBadOrder) || strings.Contains(err.Error(), "CALLERTEXTMARKER") {
			t.Errorf("%v: %v", bad, err)
		}
	}
	// Only sources in the list count, whatever else the form holds.
	if got, _ := ParseOrder([]string{"a"}, form(map[string]string{"a": "1", "z": "2"})); !slices.Equal(got, []string{"a"}) {
		t.Fatal(got)
	}
}
