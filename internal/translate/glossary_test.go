package translate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/terms"
)

type fakeGlossary struct {
	version int
	set     *terms.Set
	err     error
}

func (g fakeGlossary) Terms(context.Context, string, string) (int, *terms.Set, error) {
	return g.version, g.set, g.err
}

func withTerms(s *Service, list ...terms.Term) {
	s.Glossary = fakeGlossary{version: 5, set: terms.New(list)}
}

var (
	systemName = terms.Term{Source: "SCOS Pro", Target: "SCOS Pro"}
	secretary  = terms.Term{Source: "Chief Secretary", Target: "பிரதம செயலாளர்"}
)

// echo is a source that gives back what it was sent: a failure, under R-TRN-09.
func echo(f *fakeSources) {
	f.answer = func(sources.Piece) (sources.Result, error) { return sources.Result{}, &sources.Exhausted{} }
}

// The point of "keep as written": a lone system name came out the same from a source, which counted
// as failed (R-TRN-09), so it could not be translated at all. The entry answers it, whatever the
// sources do, with nothing sent.
func TestALoneSystemNameIsAnsweredByItsKeepAsWrittenEntry(t *testing.T) {
	f := &fakeSources{}
	echo(f)
	s, _ := newService(f)
	// Without the entry, it stays a failure: nothing is passed off as a translation.
	if _, fail := s.Translate(context.Background(), request("SCOS Pro")); fail == nil || fail.Reason != reasons.NoProvider {
		t.Fatalf("%+v", fail)
	}
	sent := len(f.sent)
	withTerms(s, systemName)
	for text, want := range map[string]string{
		"SCOS Pro":     "SCOS Pro",
		"scos pro":     "SCOS Pro",
		"  SCOS Pro\n": "  SCOS Pro\n",
		"SCOS Pro.":    "SCOS Pro.",
		"(SCOS Pro)":   "(SCOS Pro)",
	} {
		a, fail := s.Translate(context.Background(), request(text))
		if fail != nil {
			t.Fatalf("%q: %+v", text, fail)
		}
		p := a.Passages[0]
		if p.Text != want || p.Pieces[0].Origin != OriginGlossary || p.Machine() || !p.CheckedByPerson() || a.GlossaryVersion != 5 {
			t.Errorf("%q: %+v %d", text, p, a.GlossaryVersion)
		}
	}
	if len(f.sent) != sent {
		t.Fatal("a whole term reached a source")
	}
}

// R-GAT-02, R-TRN-04: whole terms are served under any gate state, with every source closed or gone,
// and for personal text too. Confidential text is refused all the same.
func TestWholeTermsUnderAnyGate(t *testing.T) {
	for name, f := range map[string]*fakeSources{
		"closed":     {allowed: []bumblebee.Source{}, read: true},
		"never read": {allowed: []bumblebee.Source{}, read: false},
	} {
		s, _ := newService(f)
		withTerms(s, systemName, secretary)
		for _, sensitivity := range []string{sources.Ordinary, sources.Personal} {
			a, fail := s.Translate(context.Background(), requestOf(sensitivity, "Chief Secretary", "SCOS Pro"))
			if fail != nil || a.Passages[0].Text != "பிரதம செயலாளர்" || a.Passages[1].Text != "SCOS Pro" || len(f.sent) != 0 {
				t.Fatalf("%s: %+v %+v", name, fail, a)
			}
		}
		if _, fail := s.Translate(context.Background(), requestOf(sources.Confidential, "SCOS Pro")); fail == nil || fail.Reason != reasons.NoProvincial {
			t.Fatalf("%+v", fail)
		}
	}
}

// R-GLO-03: a term in a sentence goes out as a stand-in, so a source never sees it, and comes back as
// published. "Keep as written" is never translated. What surrounds it stays as it was written.
func TestATermInASentenceIsProtected(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	withTerms(s, systemName, secretary)
	a, fail := s.Translate(context.Background(), request("The chief secretary uses SCOS Pro. Call 0771234567."))
	if fail != nil {
		t.Fatal(fail)
	}
	if len(f.sent) != 2 || f.sent[0].Text != "The ⟦G1⟧ uses ⟦G2⟧." || f.sent[1].Text != "Call ⟦M1⟧." {
		t.Fatalf("%+v", f.sent)
	}
	p := a.Passages[0]
	if p.Text != "[ta] The பிரதம செயலாளர் uses SCOS Pro. [ta] Call 0771234567." || !p.Machine() || len(p.TermsNotPlaced) != 0 {
		t.Fatalf("%+v", p)
	}
}

// A term with a full stop in it stays one, as terms are stood in for before the split.
func TestATermWithAStopIsNotSplit(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	withTerms(s, terms.Term{Source: "Dept. of Works", Target: "பணித் திணைக்களம்"})
	a, fail := s.Translate(context.Background(), request("The Dept. of Works wrote."))
	if fail != nil || len(f.sent) != 1 || f.sent[0].Text != "The ⟦G1⟧ wrote." || a.Passages[0].Text != "[ta] The பணித் திணைக்களம் wrote." {
		t.Fatalf("%+v %+v %+v", fail, f.sent, a)
	}
}

// R-GLO-03: a source that moves a stand-in or changes its brackets has it put back. One that drops it
// cannot be helped: the passage flags the term for the reader, and it is not failed.
func TestATermASourceChangedOrDropped(t *testing.T) {
	for name, c := range map[string]struct {
		answer string
		want   string
		lost   []string
	}{
		"moved":          {"⟦G1⟧ wrote today", "பிரதம செயலாளர் wrote today", nil},
		"square":         {"[G1] wrote", "பிரதம செயலாளர் wrote", nil},
		"spaced":         {"⟦ G 1 ⟧ wrote", "பிரதம செயலாளர் wrote", nil},
		"lower case":     {"(g1) wrote", "பிரதம செயலாளர் wrote", nil},
		"dropped":        {"wrote today", "wrote today", []string{"பிரதம செயலாளர்"}},
		"brackets alone": {"⟦⟧ wrote", "", nil},
	} {
		f := &fakeSources{answer: func(sources.Piece) (sources.Result, error) {
			return sources.Result{Text: c.answer, Source: "a", Version: "1"}, nil
		}}
		s, _ := newService(f)
		withTerms(s, secretary)
		mem := &memoryCache{}
		s.Cache, s.Keeper = mem, mem
		a, fail := s.Translate(context.Background(), request("The Chief Secretary wrote."))
		if name == "brackets alone" {
			if fail == nil || fail.Reason != reasons.MaskingLost {
				t.Errorf("%s: %+v", name, fail)
			}
			continue
		}
		if fail != nil {
			t.Fatalf("%s: %+v", name, fail)
		}
		p := a.Passages[0]
		if p.Text != c.want || strings.Join(p.TermsNotPlaced, ",") != strings.Join(c.lost, ",") {
			t.Errorf("%s: %+v", name, p)
		}
		// Only what came back whole is kept for next time.
		if kept := len(mem.kept) == 1; kept != (name == "moved") {
			t.Errorf("%s: %+v", name, mem.kept)
		}
	}
}

// One check covers both families: a stand-in no one can account for fails, and a masked value lost
// is still "masking lost" with a term beside it.
func TestOneCheckForBothFamilies(t *testing.T) {
	for name, answer := range map[string]string{
		"an invented stand-in": "⟦G1⟧ ⟦M1⟧ ⟦G9⟧",
		"a value lost":         "⟦G1⟧ only",
	} {
		f := &fakeSources{answer: func(sources.Piece) (sources.Result, error) {
			return sources.Result{Text: answer, Source: "a", Version: "1"}, nil
		}}
		s, _ := newService(f)
		withTerms(s, secretary)
		if _, fail := s.Translate(context.Background(), request("The Chief Secretary on 0771234567.")); fail == nil || fail.Reason != reasons.MaskingLost {
			t.Errorf("%s: %+v", name, fail)
		}
	}
}

// A caller who writes a stand-in's shape keeps it: their own word is not a term, nor taken for one.
func TestAStandInTheCallerWrote(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	withTerms(s, secretary)
	a, fail := s.Translate(context.Background(), request("Chief Secretary and ⟦G1⟧ here"))
	if fail != nil || f.sent[0].Text != "⟦G2⟧ and ⟦G1⟧ here" || a.Passages[0].Text != "[ta] பிரதம செயலாளர் and ⟦G1⟧ here" {
		t.Fatalf("%+v %+v %+v", fail, f.sent, a)
	}
}

// R-PRV-04: split again, each part numbers its own, and each term still comes back.
func TestASplitAgainKeepsItsTerms(t *testing.T) {
	first := true
	f := &fakeSources{}
	f.answer = func(p sources.Piece) (sources.Result, error) {
		if first {
			first = false
			return sources.Result{}, &sources.TooLong{Limit: 16}
		}
		return sources.Result{Text: "[ta] " + p.Text, Source: "a", Version: "1"}, nil
	}
	s, _ := newService(f)
	withTerms(s, secretary, systemName)
	mem := &memoryCache{}
	s.Cache, s.Keeper = mem, mem
	q := request("Chief Secretary said so then SCOS Pro did so on 0771234567 too")
	a, fail := s.Translate(context.Background(), q)
	if fail != nil {
		t.Fatal(fail)
	}
	text := a.Passages[0].Text
	for _, want := range []string{"பிரதம செயலாளர்", "SCOS Pro", "0771234567"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in %q", want, text)
		}
	}
	calls := len(f.sent)
	again, fail := s.Translate(context.Background(), q)
	if fail != nil || again.Passages[0].Text != text || len(f.sent) != calls {
		t.Fatalf("%+v %q %d %d", fail, again.Passages[0].Text, calls, len(f.sent))
	}
}

// R-TRN-08: the cache holds the stand-in, so the same sentence about another term is one row, and a
// corrected term needs nothing sent again. The cache's words never hold a term.
func TestTheCacheHoldsTheStandInNotTheTerm(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	withTerms(s, secretary, terms.Term{Source: "Auditor", Target: "கணக்காய்வாளர்"})
	mem := &memoryCache{}
	s.Cache, s.Keeper = mem, mem
	s.Translate(context.Background(), request("The Chief Secretary wrote."))
	calls := len(f.sent)
	a, fail := s.Translate(context.Background(), request("The Auditor wrote."))
	if fail != nil || len(f.sent) != calls || a.Passages[0].Text != "[ta] The கணக்காய்வாளர் wrote." || a.Passages[0].Pieces[0].Origin != OriginCache {
		t.Fatalf("%+v %d %+v", fail, len(f.sent), a)
	}
	for _, k := range mem.kept {
		if strings.Contains(k.Normal+k.Translation, "Secretary") || strings.Contains(k.Translation, "செயலாளர்") {
			t.Fatalf("%+v", k)
		}
	}
	withTerms(s, terms.Term{Source: "Chief Secretary", Target: "தலைமைச் செயலாளர்"})
	if a, _ := s.Translate(context.Background(), request("The Chief Secretary wrote.")); a.Passages[0].Text != "[ta] The தலைமைச் செயலாளர் wrote." {
		t.Fatalf("%+v", a)
	}
}

// R-NFR-05: the same word however it was typed; the rest of the text stays as it was written.
func TestATermTypedAnotherWayIsTheSameTerm(t *testing.T) {
	f := &fakeSources{}
	s, _ := newService(f)
	withTerms(s, terms.Term{Source: terms.Normalise("Café Board"), Target: "சபை"})
	a, fail := s.Translate(context.Background(), request("Café Board and café here."))
	if fail != nil || f.sent[0].Text != "⟦G1⟧ and café here." || a.Passages[0].Text != "[ta] சபை and café here." {
		t.Fatalf("%+v %+v %+v", fail, f.sent, a)
	}
}

// R-TRN-06: a glossary that cannot be read fails the request, as its version could not be stated, and
// logs fixed words.
func TestAGlossaryThatCannotBeRead(t *testing.T) {
	f := &fakeSources{}
	s, logs := newService(f)
	s.Glossary = fakeGlossary{err: errors.New(marker)}
	if _, fail := s.Translate(context.Background(), request("An invented "+marker+".")); fail == nil || fail.Reason != reasons.NoProvider {
		t.Fatalf("%+v", fail)
	}
	if strings.Contains(logs.String(), marker) || len(f.sent) != 0 {
		t.Fatal(logs)
	}
}

// Against the stand-in: every failure mode, with the change-term mode changing the stand-in itself.
func TestTermsAgainstTheStandIn(t *testing.T) {
	w := newWorld(t)
	withTerms(w.service, secretary, systemName)
	ctx := context.Background()
	q := Request{Ref: "r-1", From: "en", To: "ta", Sensitivity: sources.Ordinary, Passages: []string{"The Chief Secretary wrote."}}
	w.mode("normal", nil)
	a, fail := w.service.Translate(ctx, q)
	if fail != nil || a.Passages[0].Text != "[ta] The பிரதம செயலாளர் wrote." {
		t.Fatalf("%+v %+v", fail, a)
	}
	if got := w.received(); strings.Contains(got, "Chief Secretary") || !strings.Contains(got, "⟦G1⟧") {
		t.Fatal(got)
	}
	w.mode("change-term", map[string]string{"from": "⟦G1⟧", "to": "[G1]"})
	if a, fail = w.service.Translate(ctx, q); fail != nil || a.Passages[0].Text != "[ta] The பிரதம செயலாளர் wrote." {
		t.Fatalf("changed: %+v %+v", fail, a)
	}
	w.mode("change-term", map[string]string{"from": "⟦G1⟧", "to": "something"})
	if a, fail = w.service.Translate(ctx, q); fail != nil || a.Passages[0].Text != "[ta] The something wrote." || a.Passages[0].TermsNotPlaced[0] != "பிரதம செயலாளர்" {
		t.Fatalf("dropped: %+v %+v", fail, a)
	}
	// Every source echoing: a sentence stays a failure, while the lone name is answered.
	w.mode("echo", nil)
	if _, fail := w.service.Translate(ctx, q); fail == nil || fail.Reason != reasons.NoProvider {
		t.Fatalf("%+v", fail)
	}
	q.Passages = []string{"SCOS Pro"}
	if a, fail := w.service.Translate(ctx, q); fail != nil || a.Passages[0].Text != "SCOS Pro" || a.Passages[0].Pieces[0].Origin != OriginGlossary {
		t.Fatalf("%+v %+v", fail, a)
	}
	if strings.Contains(w.logs.String(), "SCOS Pro") || strings.Contains(w.logs.String(), "Chief") {
		t.Fatal(w.logs.String())
	}
}
