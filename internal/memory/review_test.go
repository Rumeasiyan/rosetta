package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/settings"
)

func TestACorrectionKeepsItsStandIns(t *testing.T) {
	v := settings.Defaults()
	ok := map[string]string{
		"Call ⟦M1⟧ now.|⟦M1⟧ அழைக்க.":    "⟦M1⟧ அழைக்க.",
		"A ⟦M1⟧ B ⟦M2⟧.|⟦M2⟧ பி ⟦M1⟧ ஏ.": "⟦M2⟧ பி ⟦M1⟧ ஏ.",
		"No marks.|  words\nhere  ":      "words\nhere",
		"No marks.|Café here":           "Café here",
	}
	for in, want := range ok {
		source, text, _ := strings.Cut(in, "|")
		if got, err := CleanCorrection(source, text, v); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for name, in := range map[string]string{
		"blank":       "No marks.|  ",
		"only a mark": "Call ⟦M1⟧.|⟦M1⟧",
		"lost":        "Call ⟦M1⟧.|அழைக்க",
		"twice":       "Call ⟦M1⟧.|⟦M1⟧ ⟦M1⟧ அழைக்க",
		"changed":     "Call ⟦M1⟧.|⟦M2⟧ அழைக்க",
		"invented":    "No marks.|அழைக்க ⟦M1⟧",
		"a phone":     "No marks.|அழைக்க 0771234567",
		"a card":      "No marks.|அட்டை 123456789V",
		"control":     "No marks.|bad\x01char",
		"not utf-8":   "No marks.|bad\xff",
		"too long":    "No marks.|" + strings.Repeat("அ", settings.PassageCharsMax.Default+1),
	} {
		source, text, _ := strings.Cut(in, "|")
		if _, err := CleanCorrection(source, text, v); !errors.Is(err, ErrBadText) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestAnApproversPost(t *testing.T) {
	if p, ok := CleanPost(" Café Officer "); !ok || p != "Café Officer" {
		t.Fatalf("%q %v", p, ok)
	}
	for _, bad := range []string{"", "  ", strings.Repeat("a", 201), "a\nb", "⟦M1⟧", "123456789V"} {
		if _, ok := CleanPost(bad); ok {
			t.Errorf("%q", bad)
		}
	}
}

func TestSentencesLineUp(t *testing.T) {
	list, ok := pieces(Passage{Source: "One. Two.\n\nThree 0771234567.", Approved: "ஒன்று. இரண்டு.\n\nமூன்று 077 123 4567."})
	if !ok || len(list) != 3 || list[2].source != "Three ⟦M1⟧." || list[2].approved != "மூன்று ⟦M1⟧." {
		t.Fatalf("%+v %v", list, ok)
	}
	if _, ok := pieces(Passage{Source: "One. Two.", Approved: "ஒன்று."}); ok {
		t.Fatal("unequal sentences lined up")
	}
	if _, ok := pieces(Passage{Source: "Ring 0771234567.", Approved: "0719999999."}); ok {
		t.Fatal("different numbers lined up")
	}
	if list, ok := pieces(Passage{Source: "42.", Approved: "42."}); !ok || len(list) != 0 {
		t.Fatalf("%+v", list)
	}
}
