package glossary

import (
	"errors"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
)

// R-NFR-05: the same word is one text, whichever way it was typed.
func TestATermIsNormalisedBeforeItIsKept(t *testing.T) {
	composed, decomposed := "é", "é"
	a, err := CleanTerm("  Caf" + decomposed + " ")
	if err != nil || a != "Caf"+composed {
		t.Fatalf("%q %v", a, err)
	}
	if n, err := CleanNote(" Caf" + decomposed); err != nil || n != "Caf"+composed {
		t.Fatalf("%q %v", n, err)
	}
}

func TestWhatATermMayNotBe(t *testing.T) {
	for name, s := range map[string]string{
		"blank":        "   ",
		"too long":     strings.Repeat("a", 501),
		"a line break": "one\ntwo",
		"a bracket":    "a ⟦G1⟧",
		"no letter":    "(brackets)",
		"a card":       "Number 123456789V",
		"a phone":      "Call 0771234567",
		"not utf-8":    "a\xff",
	} {
		if _, err := CleanTerm(s); !errors.Is(err, ErrBadText) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := CleanTerm(strings.Repeat("அ", 500)); err != nil {
		t.Error("500 characters of Tamil are allowed, in characters and not bytes")
	}
	if _, err := CleanNote(strings.Repeat("a", 501)); !errors.Is(err, ErrBadNote) {
		t.Error(err)
	}
	if _, err := CleanNote("Card 123456789V"); !errors.Is(err, ErrBadNote) {
		t.Error(err)
	}
}

func TestAnEntryKeptAsWrittenReadsTheSame(t *testing.T) {
	p, err := Proposal{Type: TypeKeep, Texts: map[i18n.Lang]string{i18n.EN: "SCOS Pro"}}.parse()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range i18n.Langs {
		if p.Texts[l] != "SCOS Pro" {
			t.Errorf("%s: %q", l, p.Texts[l])
		}
	}
	_, err = Proposal{Type: TypeKeep, Texts: map[i18n.Lang]string{i18n.EN: "SCOS Pro", i18n.TA: "ஸ்கோஸ்"}}.parse()
	if !errors.Is(err, ErrKeepDiffers) {
		t.Fatal(err)
	}
}

func TestLikeTakesItsMarksAsWritten(t *testing.T) {
	if got := Like(`50%_a\b`); got != `%50\%\_a\\b%` {
		t.Fatal(got)
	}
}
