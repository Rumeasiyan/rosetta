package i18n

import (
	"regexp"
	"testing"
)

func TestEveryMessageHasThreeLanguages(t *testing.T) {
	for _, k := range Keys() {
		if m := Missing(k); len(m) > 0 {
			t.Errorf("%s lacks %v", k, m)
		}
	}
}

// Sinhala and Tamil messages are in their own scripts, never left in English.
func TestMessagesAreInTheirOwnScripts(t *testing.T) {
	sinhala, tamil := regexp.MustCompile(`\p{Sinhala}`), regexp.MustCompile(`\p{Tamil}`)
	for _, k := range Keys() {
		if !sinhala.MatchString(T(SI, k)) {
			t.Errorf("%s: no Sinhala in %q", k, T(SI, k))
		}
		if !tamil.MatchString(T(TA, k)) {
			t.Errorf("%s: no Tamil in %q", k, T(TA, k))
		}
	}
}

func TestChoose(t *testing.T) {
	if Choose("ta", []Lang{SI}) != TA {
		t.Error("a pick wins")
	}
	if Choose("fr", []Lang{SI}) != SI {
		t.Error("an unknown pick is ignored")
	}
	if Choose("", nil) != EN {
		t.Error("English last")
	}
	got := Accepted("fr, ta-LK;q=0.5, si-LK;q=0.9, en;q=0")
	if len(got) != 2 || got[0] != SI || got[1] != TA {
		t.Errorf("%v", got)
	}
}

func TestMissingKeyShowsItself(t *testing.T) {
	if T(EN, "noSuchKey") != "noSuchKey" {
		t.Error("a missing key must show")
	}
}
