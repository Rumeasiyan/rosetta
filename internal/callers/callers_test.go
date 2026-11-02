package callers

import (
	"errors"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/tva"
)

func registerFile(t *testing.T) tva.File {
	t.Helper()
	f, err := tva.Parse([]byte(`{"format":1,"systems":[
		{"reference":"A","name_en":"Live","status":"live","owner_post_reference":"31","owner_subject":"s-1"},
		{"reference":"B","name_en":"May join","status":"may_join","owner_post_reference":"31","owner_subject":null},
		{"reference":"C","name_en":"Planned","status":"planned","owner_subject":"s-1"},
		{"reference":"D","name_en":"Being built","status":"in_development","owner_subject":"s-1"},
		{"reference":"E","name_en":"Retired","status":"retired","owner_subject":"s-1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestChooseTakesOnlyLiveOrMayJoin(t *testing.T) {
	f := registerFile(t)
	for ref, want := range map[string]error{"A": nil, "B": nil, "C": ErrNotAllowed, "D": ErrNotAllowed, "E": ErrNotAllowed, "Z": ErrUnknownSystem, "a": ErrUnknownSystem} {
		if _, err := Choose(f, ref); !errors.Is(err, want) {
			t.Errorf("%s: %v", ref, err)
		}
	}
}

// A null subject is the flag in R-CAL-02, whatever the post.
func TestFlagFor(t *testing.T) {
	f := registerFile(t)
	a, _ := f.Find("A")
	b, _ := f.Find("B")
	if FlagFor(a) != FlagNone || FlagFor(b) != FlagNoOwner {
		t.Fatal(FlagFor(a), FlagFor(b))
	}
}

func TestRefusalsHoldNothingTyped(t *testing.T) {
	_, err := Choose(registerFile(t), "CALLERTEXTMARKER")
	if err == nil || strings.Contains(err.Error(), "CALLERTEXTMARKER") {
		t.Fatal(err)
	}
}

func TestFitCountsCharacters(t *testing.T) {
	if got := fit(strings.Repeat("த", 300), 200); len([]rune(got)) != 200 {
		t.Fatal(len([]rune(got)))
	}
	if fit("short", 200) != "short" {
		t.Fatal("changed")
	}
}
