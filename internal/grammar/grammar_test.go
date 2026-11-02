package grammar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

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

// ---- positions (R-GRM-03, E-GRM-04) --------------------------------------------

// Text outside the Basic Multilingual Plane before and after a masked value, and across a
// split: each suggestion marks what it was made for, in the text the caller sent.
func TestPositionsAreTrueToTheTextSent(t *testing.T) {
	for name, c := range map[string]struct {
		text  string
		limit int
		want  int
	}{
		"plain":                  {"We recieve letters.", 0, 1},
		"emoji before":           {"😀😀 We recieve letters.", 0, 1},
		"emoji after":            {"We recieve 😀 letters. 😀 recieve", 0, 2},
		"value before":           {"Call 0771234567 and recieve it.", 0, 1},
		"value after":            {"We recieve at 0771234567 today.", 0, 1},
		"emoji and values":       {"😀 Card 123456789V 😀 and recieve 😀 at 0771234567 😀 recieve.", 0, 2},
		"Tamil and Sinhala":      {"வணக்கம் 😀 recieve ආයුබෝවන් 0771234567 recieve", 0, 2},
		"values of other length": {"Call +94 77 123 4567 or 077-123-4567 and recieve. Card 1990-1234-5678 recieve.", 0, 2},
		"across a split":         {strings.Repeat("😀 We recieve at 0771234567 today. ", 6), 40, 6},
		"split at a line":        {"😀 We recieve one.\n\n😀 0771234567 recieve two.\n\nAnd recieve three 😀.", 25, 3},
		"many values in a piece": {strings.Repeat("0771234567 ", 12) + "recieve " + strings.Repeat("123456789V ", 12) + "recieve", 0, 2},
		"many values, split":     {strings.Repeat("0771234567 recieve. ", 15), 30, 15},
		"a stand-in as written":  {"The form ⟦M1⟧ and 0771234567 recieve.", 0, 1},
	} {
		f := &fake{limit: c.limit}
		a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: c.text})
		if fail != nil {
			t.Errorf("%s: %v", name, fail)
			continue
		}
		if len(a.Suggestions) != c.want {
			t.Errorf("%s: %d suggestions, sent %q", name, len(a.Suggestions), f.sent)
		}
		for _, s := range a.Suggestions {
			if got := at(c.text, s); got != "recieve" {
				t.Errorf("%s: %+v marks %q", name, s, got)
			}
		}
		for _, sent := range f.sent {
			if strings.Contains(sent, "0771234567") || strings.Contains(sent, "123456789V") {
				t.Errorf("%s: a value left the server: %q", name, sent)
			}
		}
	}
}

// A hand count, so the helper above is not the only witness.
func TestPositionsByHand(t *testing.T) {
	const text = "😀 0771234567 😀 recieve"
	a, fail := service(&fake{}).Check(context.Background(), Request{Language: "en", Text: text})
	if fail != nil {
		t.Fatal(fail)
	}
	// 😀 2, space 1, the number 10, space 1, 😀 2, space 1: 17.
	if len(a.Suggestions) != 1 || a.Suggestions[0].Offset != 17 || a.Suggestions[0].Length != 7 {
		t.Fatalf("%+v", a.Suggestions)
	}
}

func TestWhatCannotBePlacedIsDropped(t *testing.T) {
	const text = "😀 Call 0771234567 now."
	// In what is sent: 😀 is 0 and 1, the stand-in ⟦M1⟧ is 8 to 11, "now" is 13.
	good := bumblebee.Suggestion{Start: 13, Length: 3, Message: "good", Replacements: []string{"then"}}
	for name, bad := range map[string]bumblebee.Suggestion{
		"negative":              {Start: -1, Length: 3, Message: "x"},
		"no length":             {Start: 13, Length: 0, Message: "x"},
		"negative length":       {Start: 13, Length: -3, Message: "x"},
		"past the end":          {Start: 13, Length: 100, Message: "x"},
		"starts past the end":   {Start: 100, Length: 1, Message: "x"},
		"inside a stand-in":     {Start: 9, Length: 1, Message: "x"},
		"starts in a stand-in":  {Start: 10, Length: 5, Message: "x"},
		"ends in a stand-in":    {Start: 3, Length: 7, Message: "x"},
		"the stand-in exactly":  {Start: 8, Length: 4, Message: "x"},
		"around a stand-in":     {Start: 3, Length: 12, Message: "x"},
		"starts inside a pair":  {Start: 1, Length: 2, Message: "x"},
		"ends inside a pair":    {Start: 0, Length: 1, Message: "x"},
		"its message is masked": {Start: 3, Length: 4, Message: "Did you mean ⟦M1⟧?"},
	} {
		f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) { return []bumblebee.Suggestion{bad, good}, nil }}
		a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: text})
		if fail != nil {
			t.Errorf("%s: %v", name, fail)
			continue
		}
		if len(a.Suggestions) != 1 || a.Suggestions[0].Message != "good" || at(text, a.Suggestions[0]) != "now" {
			t.Errorf("%s: %+v", name, a.Suggestions)
		}
	}
	// Right next to a stand-in is not touching it.
	for name, near := range map[string]bumblebee.Suggestion{
		"ends where it begins": {Start: 3, Length: 5, Message: "near"},
		"begins where it ends": {Start: 12, Length: 1, Message: "near"},
		"the whole pair":       {Start: 0, Length: 2, Message: "near"},
	} {
		f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) { return []bumblebee.Suggestion{near}, nil }}
		a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: text})
		if fail != nil || len(a.Suggestions) != 1 {
			t.Errorf("%s: %+v %v", name, a.Suggestions, fail)
		}
	}
}

func TestAReplacementHoldingAStandInGoes(t *testing.T) {
	f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) {
		return []bumblebee.Suggestion{{Start: 0, Length: 4, Message: "m", Replacements: []string{"⟦M1⟧", "Ring", "Dial ⟦M2⟧", "Phone", "Buzz"}}}, nil
	}}
	a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: "Call 0771234567 now."})
	if fail != nil {
		t.Fatal(fail)
	}
	got := a.Suggestions[0].Replacements
	if len(got) != 3 || got[0] != "Ring" || got[1] != "Phone" || got[2] != "Buzz" {
		t.Fatalf("%q", got)
	}
}

func TestSuggestionsComeInOrderWithNoOverlap(t *testing.T) {
	f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) {
		return []bumblebee.Suggestion{
			{Start: 10, Length: 3, Message: "third"},
			{Start: 0, Length: 4, Message: "first"},
			{Start: 2, Length: 4, Message: "overlaps the first"},
			{Start: 5, Length: 4, Message: "second"},
		}, nil
	}}
	a, fail := service(f).Check(context.Background(), Request{Language: "en", Text: "Some text with many words."})
	if fail != nil {
		t.Fatal(fail)
	}
	var got []string
	for _, s := range a.Suggestions {
		got = append(got, s.Message)
	}
	if strings.Join(got, ",") != "first,second,third" {
		t.Fatalf("%v", got)
	}
}

// Fuzzing the mapping: whatever the text, what a suggestion marks is what the source marked.
func FuzzPlacedSuggestionsMarkTheSameWord(f *testing.F) {
	f.Add("😀 0771234567 recieve 123456789V", 12)
	f.Add("recieve\n\n😀 recieve. recieve", 8)
	f.Add("⟦M1⟧ 0771234567 ⟦M2⟧ recieve", 9)
	f.Fuzz(func(t *testing.T, text string, limit int) {
		if !utf8.ValidString(text) || limit < 8 || limit > 2000 || len(text) > 2000 {
			t.Skip()
		}
		src := &fake{limit: limit}
		a, fail := service(src).Check(context.Background(), Request{Language: "en", Text: text})
		if fail != nil {
			t.Skip()
		}
		for _, s := range a.Suggestions {
			if got := at(text, s); got != "recieve" {
				t.Fatalf("%+v marks %q in %q, limit %d", s, got, text, limit)
			}
		}
	})
}

// ---- held suggestions (R-GRM-05) ------------------------------------------------

func TestAnIdenticalTextIsAnsweredFromMemory(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	f := &fake{}
	s := service(f)
	s.Held = &Held{Now: func() time.Time { return now }}
	check := func(text string) Answer {
		t.Helper()
		a, fail := s.Check(context.Background(), Request{Language: "en", Text: text})
		if fail != nil {
			t.Fatal(fail)
		}
		return a
	}
	const text = "We recieve letters."
	first := check(text)
	if again := check(text); len(f.sent) != 1 || len(again.Suggestions) != 1 || again.Suggestions[0].Offset != first.Suggestions[0].Offset {
		t.Fatalf("%d calls: %+v", len(f.sent), again)
	}
	check(text + " ")
	if len(f.sent) != 2 {
		t.Fatal("another text is another check")
	}
	// Ten minutes by default, and then forgotten.
	now = now.Add(10*time.Minute - time.Second)
	check(text)
	if len(f.sent) != 2 {
		t.Fatal("too early")
	}
	now = now.Add(time.Second)
	check(text)
	if len(f.sent) != 3 {
		t.Fatal("kept too long")
	}
	// The setting moves it.
	v := settings.Defaults()
	v[settings.GrammarHoldMinutes.Name] = 1
	s.Settings = func(context.Context) settings.Values { return v }
	now = now.Add(time.Minute)
	check(text)
	if len(f.sent) != 4 {
		t.Fatal("the setting was not read")
	}
	// A new process has none of it.
	s.Held = &Held{Now: func() time.Time { return now }}
	check(text)
	if len(f.sent) != 5 {
		t.Fatal("it outlived a restart")
	}
}

func TestAFailureIsNotHeld(t *testing.T) {
	calls := 0
	f := &fake{answer: func(text string) ([]bumblebee.Suggestion, error) {
		calls++
		if calls == 1 {
			return nil, &sources.Exhausted{}
		}
		return find(text, "recieve"), nil
	}}
	s := service(f)
	s.Held = &Held{}
	if _, fail := s.Check(context.Background(), Request{Language: "en", Text: "We recieve."}); fail == nil {
		t.Fatal("no failure")
	}
	if s.Held.Len() != 0 {
		t.Fatal("a failure was held")
	}
	if a, fail := s.Check(context.Background(), Request{Language: "en", Text: "We recieve."}); fail != nil || len(a.Suggestions) != 1 {
		t.Fatalf("%+v %v", a, fail)
	}
	if s.Held.Len() != 1 {
		t.Fatal(s.Held.Len())
	}
}

// Nothing of the text is in what is held.
func TestHeldHoldsNoText(t *testing.T) {
	f := &fake{answer: func(string) ([]bumblebee.Suggestion, error) {
		return []bumblebee.Suggestion{{Start: 0, Length: 4, Message: "A fixed message", Replacements: []string{"Other"}, Category: "style"}}, nil
	}}
	s := service(f)
	s.Held = &Held{}
	text := "Some " + marker + " 0771234567 words."
	if _, fail := s.Check(context.Background(), Request{Language: "en", Text: text}); fail != nil {
		t.Fatal(fail)
	}
	sum := sha256.Sum256([]byte(text))
	dump := fmt.Sprintf("%+v %x", s.Held.byKey, s.Held.order.Front().Value.(*heldEntry).key)
	dump += fmt.Sprintf("%+v", s.Held.order.Front().Value.(*heldEntry).answer)
	for _, part := range []string{marker, "0771234567", "Some", "words", hex.EncodeToString(sum[:])} {
		if strings.Contains(dump, part) {
			t.Fatalf("%q is in what is held: %s", part, dump)
		}
	}
	// And the salt keeps one process's key from meaning anything in another.
	other := &Held{}
	other.start()
	if other.key(text) == s.Held.key(text) {
		t.Fatal("the same key in two processes")
	}
}

func TestHeldStaysUnderItsCap(t *testing.T) {
	big := Answer{Suggestions: []Suggestion{{Message: strings.Repeat("m", 300)}}}
	one := sizeOf(big)
	h := &Held{Max: one * 3}
	for i := range 10 {
		h.Put(fmt.Sprint("text ", i), big)
		if h.size > h.Max {
			t.Fatalf("%d over %d", h.size, h.Max)
		}
	}
	if h.Len() != 3 {
		t.Fatal(h.Len())
	}
	if _, ok := h.Get("text 0", time.Hour); ok {
		t.Fatal("the oldest goes first")
	}
	if _, ok := h.Get("text 9", time.Hour); !ok {
		t.Fatal("the newest stays")
	}
	h.Put("text 9", big)
	if h.Len() != 3 || h.size != one*3 {
		t.Fatal("the same text counts once")
	}
	// Too big to hold at all is simply not held, and takes nothing with it.
	h.Put("huge", Answer{Suggestions: []Suggestion{{Message: strings.Repeat("m", one*4)}}})
	if _, ok := h.Get("huge", time.Hour); ok || h.Len() != 3 {
		t.Fatal(h.Len())
	}
}

func TestSweepForgetsWithNobodyAsking(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	h := &Held{Now: func() time.Time { return now }}
	h.Put("older", Answer{})
	now = now.Add(5 * time.Minute)
	h.Put("newer", Answer{})
	now = now.Add(5 * time.Minute)
	h.SweepWith(context.Background(), func(context.Context) settings.Values { return settings.Defaults() })
	if h.Len() != 1 {
		t.Fatal(h.Len())
	}
	if _, ok := h.Get("newer", time.Hour); !ok {
		t.Fatal("swept too soon")
	}
	now = now.Add(5 * time.Minute)
	h.Sweep(10 * time.Minute)
	if h.Len() != 0 || h.size != 0 {
		t.Fatal(h.Len(), h.size)
	}
}

func TestHeldUnderManyHands(t *testing.T) {
	h := &Held{Max: 4096}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 200 {
				k := fmt.Sprint(i, j%20)
				h.Put(k, Answer{})
				h.Get(k, time.Hour)
				if j%50 == 0 {
					h.Sweep(time.Hour)
				}
			}
		})
	}
	wg.Wait()
	if h.size > h.Max {
		t.Fatal(h.size)
	}
}

// R-GRM-05 says never to disk or a record. The package that holds the text cannot do either:
// it imports nothing that could.
func TestThisPackageCannotWriteAnywhere(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal(files, err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if path == "os" || path == "io/ioutil" || path == "database/sql" || strings.Contains(path, "pgx") || strings.Contains(path, "internal/audit") {
				t.Errorf("%s imports %s", name, path)
			}
		}
	}
}
