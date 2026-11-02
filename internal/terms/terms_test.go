package terms

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func find(s *Set, text string) []string {
	var out []string
	for _, f := range s.Find(text) {
		out = append(out, text[f.Start:f.End]+"="+f.Term.Target)
	}
	return out
}

func same(t *testing.T, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFindsWholeWordsInAnyCaseTheLongestFirst(t *testing.T) {
	s := New([]Term{{"Chief Secretary", "CS"}, {"Chief", "C"}, {"SCOS Pro", "SCOS Pro"}, {"Form 12", "F12"}, {"A.G.A.", "AGA"}})
	same(t, find(s, "The chief secretary wrote."), "chief secretary=CS")
	same(t, find(s, "The Chief wrote to the CHIEF  SECRETARY."), "Chief=C", "CHIEF  SECRETARY=CS")
	same(t, find(s, "Mischief is not a Chiefdom."))
	same(t, find(s, "SCOS Pro, and scos pro!"), "SCOS Pro=SCOS Pro", "scos pro=SCOS Pro")
	same(t, find(s, "Form 123 and Form 12."), "Form 12=F12")
	same(t, find(s, "To the A.G.A. today"), "A.G.A.=AGA")
	same(t, find(s, "Chief\nSecretary"), "Chief=C")
}

// In Sinhala and Tamil the vowels are marks; a word goes on through them, and a case ending makes a
// different word, which is not matched.
func TestTheThreeScripts(t *testing.T) {
	s := New([]Term{{"பிரதம செயலாளர்", "Chief Secretary"}, {"ප්‍රධාන ලේකම්", "Chief Secretary"}})
	same(t, find(s, "இன்று பிரதம செயலாளர் வந்தார்."), "பிரதம செயலாளர்=Chief Secretary")
	same(t, find(s, "பிரதம செயலாளருக்கு அனுப்புக."))
	same(t, find(s, "අද ප්‍රධාන ලේකම් පැමිණියේය."), "ප්‍රධාන ලේකම්=Chief Secretary")
}

// R-NFR-05: typed either way, the same word; and the offsets are of the text as written.
func TestTheSameWordHoweverItWasTyped(t *testing.T) {
	s := New([]Term{{Normalise("Café"), "Kafe"}})
	text := "A café and a Café here"
	got := s.Find(text)
	if len(got) != 2 || text[got[0].Start:got[0].End] != "café" || text[got[1].Start:got[1].End] != "Café" {
		t.Fatalf("%+v", got)
	}
	// The other way about: a term stored as typed still finds its word.
	s = New([]Term{{"Café", "Kafe"}})
	if len(s.Find("Café")) != 1 {
		t.Fatal("not found")
	}
	if Normalise("é") != "é" {
		t.Fatal("not composed")
	}
}

func TestAStandInIsNeverSearched(t *testing.T) {
	s := New([]Term{{"M1", "x"}, {"G1", "y"}})
	same(t, find(s, "Call ⟦M1⟧ or ⟦G1⟧ about M1."), "M1=x")
}

func TestOutsideTheBasicPlaneAndAtTheEdges(t *testing.T) {
	s := New([]Term{{"Term", "T"}})
	text := "𝒜 Term 𝒜"
	got := s.Find(text)
	if len(got) != 1 || text[got[0].Start:got[0].End] != "Term" {
		t.Fatalf("%+v", got)
	}
	same(t, find(s, "Term"), "Term=T")
	same(t, find(s, "  Term  "), "Term=T")
	same(t, find(New(nil), "Term"))
	var none *Set
	if none.Len() != 0 {
		t.Fatal(none.Len())
	}
}

func TestATermWithNothingToFindByIsLeftOut(t *testing.T) {
	s := New([]Term{{"(brackets)", "x"}, {"Fine", ""}, {"Good", "y"}})
	if s.Len() != 1 {
		t.Fatal(s.Len())
	}
}

// Finding is a walk through the text, not through every term: a full glossary stays fast.
func BenchmarkFindInAFullGlossary(b *testing.B) {
	list := make([]Term, 20000)
	for i := range list {
		list[i] = Term{Source: "term" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)) + " word", Target: "t"}
	}
	s := New(list)
	text := strings.Repeat("A plain sentence with a termabc word and some more of it. ", 80)
	b.ResetTimer()
	for b.Loop() {
		s.Find(text)
	}
}

// R-NFR-05: nothing but Normalise calls the normaliser, so text can only be normalised one way.
func TestThereIsOneNormaliseFunction(t *testing.T) {
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasPrefix(path, filepath.Join("..", "terms")) {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "unicode/norm") && !strings.HasSuffix(path, "_test.go") {
			t.Errorf("%s normalises for itself", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
