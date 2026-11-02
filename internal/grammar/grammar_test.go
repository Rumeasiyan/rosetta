package grammar

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

const marker = "GRAMMARTEXTMARKER"

// fake finds every word "recieve" in what it is sent, as a source would, and keeps what
// it was sent for a test to read. answer replaces that when set.
type fake struct {
	limit  int
	sent   []string
	answer func(text string) ([]bumblebee.Suggestion, error)
}

func (f *fake) MaxText() int {
	if f.limit > 0 {
		return f.limit
	}
	return 5000
}

// find is every place of word in text, in UTF-16 units.
func find(text, word string) []bumblebee.Suggestion {
	var out []bumblebee.Suggestion
	from := 0
	for {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return out
		}
		at := from + i
		out = append(out, bumblebee.Suggestion{Start: len(utf16.Encode([]rune(text[:at]))), Length: len(utf16.Encode([]rune(word))), Message: "Possible spelling mistake", Replacements: []string{"receive"}, Category: "spelling"})
		from = at + len(word)
	}
}

func (f *fake) Grammar(_ context.Context, p sources.GrammarPiece) (sources.GrammarResult, error) {
	f.sent = append(f.sent, p.Text)
	if f.answer != nil {
		list, err := f.answer(p.Text)
		return sources.GrammarResult{Suggestions: list, Source: "fake", Version: "1"}, err
	}
	return sources.GrammarResult{Suggestions: find(p.Text, "recieve"), Source: "fake", Version: "1"}, nil
}

func service(f *fake) *Service {
	return &Service{Sources: f, Settings: func(context.Context) settings.Values { return settings.Defaults() }}
}

// at reads a suggestion's place in the text, as the editor would.
func at(text string, s Suggestion) string {
	return string(utf16.Decode(utf16.Encode([]rune(text))[s.Offset : s.Offset+s.Length]))
}

func TestLanguages(t *testing.T) {
	for language, want := range map[string]reasons.Reason{
		"auto": "", "en": "", "en-GB": "", "en-US": "", "EN-gb": "", "en-AU": "",
		"": reasons.Invalid, " ": reasons.Invalid,
		"si": reasons.Unsupported, "ta": reasons.Unsupported, "si-LK": reasons.Unsupported, "ta-LK": reasons.Unsupported,
		"fr": reasons.Unsupported, "english": reasons.Unsupported, "en_GB": reasons.Unsupported, "en-": reasons.Unsupported,
	} {
		f := CheckLanguage(language)
		if (want == "") != (f == nil) || (f != nil && f.Reason != want) {
			t.Errorf("%q: %v", language, f)
		}
	}
}

func TestACheckFindsAndPlaces(t *testing.T) {
	f := &fake{}
	const text = "We recieve letters.\n\nThey recieve answers."
	a, fail := service(f).Check(context.Background(), Request{Ref: "r-1", Language: "en-GB", Text: text})
	if fail != nil {
		t.Fatal(fail)
	}
	if len(a.Suggestions) != 2 || at(text, a.Suggestions[0]) != "recieve" || at(text, a.Suggestions[1]) != "recieve" {
		t.Fatalf("%+v", a.Suggestions)
	}
	if a.Suggestions[0].Offset != 3 || a.Suggestions[1].Offset != 26 {
		t.Fatalf("%+v", a.Suggestions)
	}
	s := a.Suggestions[0]
	if s.Message == "" || len(s.Replacements) != 1 || s.Category != CategorySpelling || s.Source != "fake" {
		t.Fatalf("%+v", s)
	}
	if len(f.sent) != 1 {
		t.Fatalf("a short text goes as one piece: %q", f.sent)
	}
	if len(a.Sources) != 1 || a.Sources[0] != (Used{"fake", "1"}) {
		t.Fatalf("%+v", a.Sources)
	}
}

func TestNothingToSuggestIsAnEmptyListNotNil(t *testing.T) {
	for _, text := range []string{"Nothing wrong here.", "", "  \n ", "0771234567\n"} {
		a, fail := service(&fake{}).Check(context.Background(), Request{Language: "auto", Text: text})
		if fail != nil || a.Suggestions == nil || len(a.Suggestions) != 0 {
			t.Errorf("%q: %+v %v", text, a, fail)
		}
	}
}

// A piece with no words outside its stand-ins costs no source call.
func TestOnlyANumberGoesNowhere(t *testing.T) {
	f := &fake{}
	if _, fail := service(f).Check(context.Background(), Request{Language: "en", Text: "0771234567\n123456789V"}); fail != nil {
		t.Fatal(fail)
	}
	if len(f.sent) != 0 {
		t.Fatalf("%q", f.sent)
	}
}

func TestMaskingIsAlwaysOn(t *testing.T) {
	f := &fake{}
	const text = "Call 0771234567 or see 123456789V and recieve it."
	a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: text})
	if fail != nil {
		t.Fatal(fail)
	}
	if strings.Contains(f.sent[0], "0771234567") || strings.Contains(f.sent[0], "123456789V") {
		t.Fatal(f.sent)
	}
	if len(a.Suggestions) != 1 || at(text, a.Suggestions[0]) != "recieve" {
		t.Fatalf("%+v", a.Suggestions)
	}
}

// Text over the source's size goes in pieces of at most that, counted in code points, and
// each suggestion is true to the whole text.
func TestSplittingAndJoining(t *testing.T) {
	f := &fake{limit: 40}
	text := strings.Repeat("We recieve a letter here today. ", 5)
	a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: text})
	if fail != nil {
		t.Fatal(fail)
	}
	if len(f.sent) < 2 {
		t.Fatalf("%q", f.sent)
	}
	for _, p := range f.sent {
		if n := len([]rune(p)); n > 40 {
			t.Fatalf("%d code points: %q", n, p)
		}
	}
	if len(a.Suggestions) != 5 {
		t.Fatalf("%+v", a.Suggestions)
	}
	for _, s := range a.Suggestions {
		if at(text, s) != "recieve" {
			t.Fatalf("%+v at %q", s, at(text, s))
		}
	}
}

// Code points are what is limited: an emoji is one, though two units in a position.
func TestSizeIsCodePoints(t *testing.T) {
	v := settings.Defaults()
	v[settings.GrammarCharsMax.Name] = 5
	s := service(&fake{})
	s.Settings = func(context.Context) settings.Values { return v }
	if _, fail := s.Check(context.Background(), Request{Language: "en", Text: "😀😀😀😀😀"}); fail != nil {
		t.Fatal(fail)
	}
	_, fail := s.Check(context.Background(), Request{Language: "en", Text: marker})
	if fail == nil || fail.Reason != reasons.TooLong || strings.Contains(fail.Error(), marker) {
		t.Fatal(fail)
	}
}

func TestChunks(t *testing.T) {
	const text = "One two. Three four.\nFive six."
	got := Chunks(text, 100)
	if len(got) != 1 || text[got[0].Start:got[0].End] != text {
		t.Fatalf("%+v", got)
	}
	got = Chunks(text, 21)
	if len(got) != 2 || text[got[0].Start:got[0].End] != "One two. Three four." || text[got[1].Start:got[1].End] != "Five six." {
		t.Fatalf("%+v", got)
	}
	if got := Chunks("  \n ", 10); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	// A word longer than the limit is cut, never left over.
	if got := Chunks(strings.Repeat("a", 25), 10); len(got) != 3 {
		t.Fatalf("%+v", got)
	}
}

// A source saying too long, on a list that has shrunk, gets smaller pieces, once.
func TestTooLongSplitsAgain(t *testing.T) {
	f := &fake{}
	calls := 0
	f.answer = func(text string) ([]bumblebee.Suggestion, error) {
		calls++
		if calls == 1 {
			return nil, &sources.TooLong{Limit: 20}
		}
		return find(text, "recieve"), nil
	}
	const text = "We recieve one here. We recieve two here."
	a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: text})
	if fail != nil {
		t.Fatal(fail)
	}
	if len(a.Suggestions) != 2 || a.Suggestions[1].Offset != 24 || at(text, a.Suggestions[1]) != "recieve" {
		t.Fatalf("%+v", a.Suggestions)
	}
	f = &fake{answer: func(string) ([]bumblebee.Suggestion, error) { return nil, &sources.TooLong{Limit: 20} }}
	if _, fail := service(f).Check(context.Background(), Request{Language: "en", Text: text}); fail == nil || fail.Reason != reasons.TooLong {
		t.Fatal(fail)
	}
}

func TestFailuresAreReasonsWithNoText(t *testing.T) {
	retry := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, c := range map[string]struct {
		err  error
		want reasons.Reason
	}{
		"exhausted": {&sources.Exhausted{RetryAt: retry}, reasons.NoProvider},
		"too late":  {sources.ErrTooLate, reasons.TimedOut},
		"deadline":  {context.DeadlineExceeded, reasons.TimedOut},
		"unknown":   {context.Canceled, reasons.TimedOut},
	} {
		f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) { return nil, c.err }}
		a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: marker + " text."})
		if fail == nil || fail.Reason != c.want || a.Suggestions != nil || strings.Contains(fail.Error(), marker) {
			t.Errorf("%s: %v %+v", name, fail, a)
		}
	}
	f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) { return nil, &sources.Exhausted{RetryAt: retry} }}
	_, fail := service(f).Check(context.Background(), Request{Language: "en", Text: "Some text."})
	if fail.RetryAt == nil || !fail.RetryAt.Equal(retry) {
		t.Fatal(fail)
	}
}

// All or nothing, and a text that is not text is refused.
func TestOtherRefusals(t *testing.T) {
	calls := 0
	f := &fake{limit: 12, answer: func(string) ([]bumblebee.Suggestion, error) {
		calls++
		if calls == 2 {
			return nil, &sources.Exhausted{}
		}
		return []bumblebee.Suggestion{{Start: 0, Length: 1, Message: "x"}}, nil
	}}
	if a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: "First one. Second one."}); fail == nil || a.Suggestions != nil {
		t.Fatalf("%+v %v", a, fail)
	}
	if _, fail := service(&fake{}).Check(context.Background(), Request{Language: "en", Text: "bad \xff bytes"}); fail == nil || fail.Reason != reasons.Invalid {
		t.Fatal(fail)
	}
	if _, fail := service(&fake{}).Check(context.Background(), Request{Language: "si", Text: "අකුරු"}); fail == nil || fail.Reason != reasons.Unsupported {
		t.Fatal(fail)
	}
}

func TestAtMostThreeReplacementsAndKnownCategories(t *testing.T) {
	f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) {
		return []bumblebee.Suggestion{{Start: 0, Length: 2, Message: "m", Replacements: []string{"a", "b", "c", "d"}, Category: "made-up"},
			{Start: 3, Length: 2, Message: "m", Category: "style"}}, nil
	}}
	a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: "Some words here."})
	if fail != nil {
		t.Fatal(fail)
	}
	if got := a.Suggestions[0]; len(got.Replacements) != 3 || got.Category != CategoryOther {
		t.Fatalf("%+v", got)
	}
	if got := a.Suggestions[1]; got.Replacements == nil || len(got.Replacements) != 0 || got.Category != CategoryStyle {
		t.Fatalf("%+v", got)
	}
}

func TestContext(t *testing.T) {
	text := strings.Repeat("a", 100) + "recieve" + strings.Repeat("b", 100)
	e := Encode(text)
	x := e.ContextOf(Suggestion{Offset: 100, Length: 7})
	if x.Offset != 40 || x.Length != 7 || len(x.Text) != 87 || x.Text[40:47] != "recieve" {
		t.Fatalf("%+v", x)
	}
	// At the edges there is less, and a pair of units is never cut.
	x = Encode("recieve").ContextOf(Suggestion{Offset: 0, Length: 7})
	if x.Text != "recieve" || x.Offset != 0 {
		t.Fatalf("%+v", x)
	}
	emoji := strings.Repeat("😀", 30) + "recieve" + strings.Repeat("😀", 30)
	x = Encode(emoji).ContextOf(Suggestion{Offset: 61, Length: 7}) // 60 units, less one, lands in a pair
	if strings.ContainsRune(x.Text, '�') {
		t.Fatalf("%q", x.Text)
	}
	if got := string(utf16.Decode(utf16.Encode([]rune(x.Text))[x.Offset : x.Offset+x.Length])); !strings.HasPrefix(got, "ecieve") {
		t.Fatalf("%q", got)
	}
}
