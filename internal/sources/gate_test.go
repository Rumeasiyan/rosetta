package sources

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
)

var on = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func openGate(version string) Gate {
	return Gate{State: GateOpen, SourceVersion: version, TestSet: "shared-1", Score: 80, EvaluatedOn: on}
}

func TestAGateOpensOnlyForTheVersionEvaluated(t *testing.T) {
	s := bumblebee.Source{Name: "a", Version: "2"}
	for name, c := range map[string]struct {
		gates map[GateKey]Gate
		want  bool
	}{
		"no decision":  {nil, false},
		"closed":       {map[GateKey]Gate{{"a", "en", "ta"}: {State: GateClosed, SourceVersion: "2"}}, false},
		"open":         {map[GateKey]Gate{{"a", "en", "ta"}: openGate("2")}, true},
		"old version":  {map[GateKey]Gate{{"a", "en", "ta"}: openGate("1")}, false},
		"another way":  {map[GateKey]Gate{{"a", "ta", "en"}: openGate("2")}, false},
		"another name": {map[GateKey]Gate{{"b", "en", "ta"}: openGate("2")}, false},
	} {
		if (Policy{Gates: c.gates}).Open(s, "en", "ta") != c.want {
			t.Errorf("%s", name)
		}
	}
}

func TestCandidatesAreOnOpenAndAllowed(t *testing.T) {
	l := bumblebee.List{Sources: []bumblebee.Source{offering("a", enTa), offering("b", enTa), offering("c", enTa), offering("d", enTa)}}
	l.Sources[3].State = "resting-block"
	d := Translate("en", "ta")
	p := Policy{
		Gates:  map[GateKey]Gate{{"a", "en", "ta"}: openGate("1"), {"b", "en", "ta"}: openGate("1"), {"d", "en", "ta"}: openGate("1")},
		Off:    map[string]bool{"b": true},
		Orders: map[Direction][]string{d: {"d", "a"}},
	}
	if got := names(p.Candidates(l, d, Ordinary)); !slices.Equal(got, []string{"d", "a"}) {
		t.Fatal(got)
	}
	if got := names(p.Candidates(l, d, Personal)); !slices.Equal(got, []string{"d", "a"}) {
		t.Fatal(got)
	}
	if got := p.Candidates(l, d, Confidential); len(got) != 0 {
		t.Fatal("confidential text goes to no outside website", got)
	}
	if got := p.Candidates(l, d, ""); len(got) != 0 {
		t.Fatal(got)
	}
	if got := (Policy{}).Candidates(l, d, Ordinary); len(got) != 0 {
		t.Fatal("closed until opened", got)
	}
}

func TestGrammarHasNoGate(t *testing.T) {
	gr := offering("g", bumblebee.Offer{Operation: "grammar", From: "en"})
	if got := names((Policy{}).Candidates(bumblebee.List{Sources: []bumblebee.Source{gr}}, Grammar, Ordinary)); !slices.Equal(got, []string{"g"}) {
		t.Fatal(got)
	}
}

func TestStatesTellWhatIsOpen(t *testing.T) {
	later := on.AddDate(0, 1, 0)
	g := openGate("1")
	g.EvaluatedOn = later
	l := bumblebee.List{Sources: []bumblebee.Source{offering("a", enTa), offering("b", enTa)}}
	got := States(l, Policy{Gates: map[GateKey]Gate{{"a", "en", "ta"}: openGate("1"), {"b", "en", "ta"}: g}})
	if len(got) != 6 {
		t.Fatal(len(got))
	}
	for _, st := range got {
		if st.From == "en" && st.To == "ta" {
			if !st.Open || len(st.Sources) != 2 || !st.EvaluatedOn.Equal(later) {
				t.Fatalf("%+v", st)
			}
		} else if st.Open || st.EvaluatedOn != nil || len(st.Sources) != 0 {
			t.Fatalf("%+v", st)
		}
	}
}

func TestParseEvaluation(t *testing.T) {
	today := time.Date(2030, 6, 1, 0, 0, 0, 0, time.UTC)
	e, err := ParseEvaluation(" shared-set 1.2 ", " 81.5 ", "2030-06-01", today)
	if err != nil || e.TestSet != "shared-set 1.2" || e.Score != 81.5 || !e.EvaluatedOn.Equal(today) {
		t.Fatal(e, err)
	}
	for name, c := range map[string][3]string{
		"no test set": {"", "80", "2030-01-01"},
		"odd":         {"<b>", "80", "2030-01-01"},
		"long":        {strings.Repeat("a", 101), "80", "2030-01-01"},
		"no score":    {"s", "", "2030-01-01"},
		"not a score": {"s", "CALLERTEXTMARKER", "2030-01-01"},
		"nan":         {"s", "NaN", "2030-01-01"},
		"inf":         {"s", "Inf", "2030-01-01"},
		"no date":     {"s", "80", ""},
		"bad date":    {"s", "80", "soon"},
		"ahead":       {"s", "80", "2030-06-02"},
	} {
		_, err := ParseEvaluation(c[0], c[1], c[2], today)
		if !errors.Is(err, ErrBadEvaluation) || strings.Contains(err.Error(), "CALLERTEXTMARKER") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
