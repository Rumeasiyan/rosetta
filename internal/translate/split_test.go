package translate

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/mask"
)

func pieces(text string, limit int) []string {
	var out []string
	for _, s := range Split(text, limit) {
		out = append(out, text[s.Start:s.End])
	}
	return out
}

func TestSentences(t *testing.T) {
	for name, c := range map[string]struct {
		text string
		want []string
	}{
		"one":             {"An invented letter.", []string{"An invented letter."}},
		"two":             {"First one. Second one.", []string{"First one.", "Second one."}},
		"marks":           {"Really? Yes! Good… Then more", []string{"Really?", "Yes!", "Good…", "Then more"}},
		"run of marks":    {"What?! Now.", []string{"What?!", "Now."}},
		"closers":         {`He said "Go." Then left.`, []string{`He said "Go."`, "Then left."}},
		"brackets":        {"(See above.) Next.", []string{"(See above.)", "Next."}},
		"no end mark":     {"No mark at the end", []string{"No mark at the end"}},
		"line breaks":     {"Dear Sir\nThe letter.\r\n\r\nYours", []string{"Dear Sir", "The letter.", "Yours"}},
		"title":           {"Mr. Perera and Dr. Silva came. They sat.", []string{"Mr. Perera and Dr. Silva came.", "They sat."}},
		"initials":        {"A. B. Silva came. He sat.", []string{"A. B. Silva came.", "He sat."}},
		"short forms":     {"Use e.g. this one. Or i.e. that.", []string{"Use e.g. this one.", "Or i.e. that."}},
		"goes on small":   {"Call the dept. of works today.", []string{"Call the dept. of works today."}},
		"number":          {"No. 5 Main Street. Next.", []string{"No. 5 Main Street.", "Next."}},
		"the word no":     {"The answer is no. Then we left.", []string{"The answer is no.", "Then we left."}},
		"list":            {"1. First item", []string{"1. First item"}},
		"decimal":         {"It costs 3.5 units. Next.", []string{"It costs 3.5 units.", "Next."}},
		"web address":     {"See example.org for more. Next.", []string{"See example.org for more.", "Next."}},
		"sinhala":         {"මෙය පළමු වාක්‍යයයි. මෙය දෙවැන්න ඇත. තුන්වැන්න?", []string{"මෙය පළමු වාක්‍යයයි.", "මෙය දෙවැන්න ඇත.", "තුන්වැන්න?"}},
		"sinhala initial": {"ඒ. බී. සිල්වා මහතා පැමිණියේය. ඔහු හිඳගත්තේය.", []string{"ඒ. බී. සිල්වා මහතා පැමිණියේය.", "ඔහු හිඳගත්තේය."}},
		"sinhala joined":  {"පී.එස්. සිල්වා පැමිණියේය. ඔහු හිඳගත්තේය.", []string{"පී.එස්. සිල්වා පැමිණියේය.", "ඔහු හිඳගත්තේය."}},
		"tamil":           {"இது முதல் வாக்கியம். இது இரண்டாவது! மூன்றாவது?", []string{"இது முதல் வாக்கியம்.", "இது இரண்டாவது!", "மூன்றாவது?"}},
		"tamil initial":   {"எஸ். குமார் வந்தார். அவர் அமர்ந்தார்.", []string{"எஸ். குமார் வந்தார்.", "அவர் அமர்ந்தார்."}},
		"danda":           {"வாக்கியம் ஒன்று। வாக்கியம் இரண்டு", []string{"வாக்கியம் ஒன்று।", "வாக்கியம் இரண்டு"}},
		"kunddaliya":      {"පළමුවැන්න ෴ දෙවැන්න", []string{"පළමුවැන්න ෴", "දෙවැන්න"}},
		"masked":          {"Call ⟦M1⟧. Then ⟦M2⟧ now.", []string{"Call ⟦M1⟧.", "Then ⟦M2⟧ now."}},
		"nothing":         {"", nil},
		"only space":      {" \n\t ", nil},
		"only a mark":     {".", []string{"."}},
	} {
		if got := pieces(c.text, 1000); !slices.Equal(got, c.want) {
			t.Errorf("%s: %q", name, got)
		}
	}
}

// Whatever the text, the pieces and what lies between them add up to it byte for byte,
// and none is over the limit.
func TestPiecesAndGapsAreTheWholeText(t *testing.T) {
	long := strings.Repeat("word ", 40)
	for _, text := range []string{
		"  Leading and trailing  ", "\n\nDear Sir,\n\n  First one.   Second one.\t\n", "Line one\r\nLine two\r\n",
		long, "   " + long + "\n" + long, "මෙය පළමු වාක්‍යයයි.\n\nஇது இரண்டாவது.  Third.\n",
		strings.Repeat("අ", 300), strings.Repeat("க்", 300), strings.Repeat("😀", 300), "no spaces" + strings.Repeat("x", 500) + " then words",
	} {
		for _, limit := range []int{1, 3, 7, 50, 5000} {
			spans := Split(text, limit)
			var b strings.Builder
			last := 0
			for _, s := range spans {
				if s.Start < last || s.End <= s.Start {
					t.Fatalf("%q at %d: %v", text, limit, spans)
				}
				b.WriteString(text[last:s.Start])
				b.WriteString(text[s.Start:s.End])
				last = s.End
				if n := utf8.RuneCountInString(text[s.Start:s.End]); n > limit+2 {
					t.Errorf("%q at %d: a piece of %d", text, limit, n)
				}
				if strings.TrimSpace(text[s.Start:s.End]) != text[s.Start:s.End] {
					t.Errorf("%q: a piece with spacing at its edge: %q", text, text[s.Start:s.End])
				}
			}
			b.WriteString(text[last:])
			if b.String() != text {
				t.Fatalf("%q at %d became %q", text, limit, b.String())
			}
		}
	}
}

func TestLongSentencesBreakAtWordBoundaries(t *testing.T) {
	got := pieces("one two three four five six", 9)
	if !slices.Equal(got, []string{"one two", "three", "four five", "six"}) {
		t.Fatal(got)
	}
	// Counted in code points, not bytes: these are six and six.
	if got := pieces("அஆஇஈஉஊ எஏஐஒஓஔ", 6); !slices.Equal(got, []string{"அஆஇஈஉஊ", "எஏஐஒஓஔ"}) {
		t.Fatal(got)
	}
	if got := pieces("😀😀😀 😀😀😀", 3); !slices.Equal(got, []string{"😀😀😀", "😀😀😀"}) {
		t.Fatal(got)
	}
	if got := pieces("exactly ten", 11); !slices.Equal(got, []string{"exactly ten"}) {
		t.Fatal(got)
	}
}

func TestALongWordIsCutSafely(t *testing.T) {
	if got := pieces("abcdefghij", 4); !slices.Equal(got, []string{"abcd", "efgh", "ij"}) {
		t.Fatal(got)
	}
	// Never before a vowel sign or joiner: the letter and its marks stay together.
	for _, p := range pieces(strings.Repeat("කෝ", 20), 3) {
		if r, _ := utf8.DecodeRuneInString(p); isJoined(r) {
			t.Fatalf("a piece begins with a mark: %q", p)
		}
	}
}

// A stand-in is never cut, so a value is never split between two pieces: masking first
// means a spaced number cannot be broken up and sent unmasked.
func TestAStandInIsNeverCut(t *testing.T) {
	m := mask.Mask("Call 077 123 4567 and 123456789V or 0719999999 today please")
	for limit := 1; limit < 30; limit++ {
		spans := Split(m.Text, limit)
		for _, it := range m.Items {
			whole := false
			for _, s := range spans {
				if it.Start >= s.Start && it.End <= s.End {
					whole = true
				}
				if (it.Start > s.Start && it.Start < s.End && it.End > s.End) || (it.End > s.Start && it.End < s.End && it.Start < s.Start) {
					t.Fatalf("limit %d cut %q: %v", limit, it.Token, spans)
				}
			}
			if !whole {
				t.Fatalf("limit %d lost %q: %v", limit, it.Token, spans)
			}
		}
	}
	// With no spaces at all, the cut goes round it.
	withLong := "aaaaaaaa⟦M1⟧bbbbbbbb"
	for limit := 1; limit < 20; limit++ {
		for _, s := range pieces(withLong, limit) {
			if strings.Count(s, "⟦") != strings.Count(s, "⟧") {
				t.Fatalf("limit %d: %q", limit, s)
			}
		}
	}
}

func TestNormaliseMatchesTheSameWord(t *testing.T) {
	// The same Sinhala vowel sign written in one code point and in two.
	if Normalise("කො") != Normalise("කො") {
		t.Fatal("not the same word")
	}
	if Normalise("é") != "é" {
		t.Fatal("not composed")
	}
}
