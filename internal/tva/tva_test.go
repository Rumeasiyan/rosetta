package tva

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const file = `{"format":1,"systems":[
 {"reference":"EP-0012","name_si":"ගොවි ලේඛනය","name_ta":"விவசாயிகள் பதிவேடு","name_en":"Invented Register","status":"live","institution_reference":"7",
  "web_address":null,"owner_post_reference":"31","owner_subject":"subject-1","a_field_from_later":{"x":1}},
 {"reference":"EP-0040","name_si":null,"name_ta":"சோதனை","name_en":null,"status":"may_join","owner_post_reference":null,"owner_subject":null}]}`

func TestParseReadsWhatROSETTAUsesAndIgnoresTheRest(t *testing.T) {
	f, err := Parse([]byte(file))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := f.Find("EP-0012")
	if !ok || s.Name() != "Invented Register" || s.Status != StatusLive {
		t.Fatalf("%+v", s)
	}
	if sub, post := s.Owner(); sub != "subject-1" || post != "31" {
		t.Fatal(sub, post)
	}
	n, ok := f.Find("EP-0040")
	if !ok || n.Name() != "சோதனை" {
		t.Fatalf("%+v", n)
	}
	if sub, post := n.Owner(); sub != "" || post != "" {
		t.Fatal("a null owner must read as none")
	}
	if _, ok := f.Find("ep-0012"); ok {
		t.Fatal("references compare exactly")
	}
}

func TestParseRefusesAFormatItDoesNotKnow(t *testing.T) {
	for _, raw := range []string{`{"format":2,"systems":[]}`, `{"systems":[]}`, `{"format":"1","systems":[]}`, `{"format":1.5,"systems":[]}`, `{"format":null}`} {
		if _, err := Parse([]byte(raw)); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestParseFaultsHoldNothingOfTheFile(t *testing.T) {
	for _, raw := range []string{`SECRETMARKER`, `{"format":1,"systems":"SECRETMARKER"}`, `{"format":1,"systems":[{"name_en":"SECRETMARKER"}]}`} {
		_, err := Parse([]byte(raw))
		if err == nil || strings.Contains(err.Error(), "SECRETMARKER") {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

// Live and may join count; every other status does not (R-CAL-01).
func TestMayCall(t *testing.T) {
	for status, want := range map[string]bool{StatusLive: true, StatusMayJoin: true, StatusPlanned: false, StatusInDevelopment: false, StatusRetired: false, "": false, "from-later": false} {
		if MayCall(status) != want {
			t.Errorf("%q", status)
		}
	}
}

func TestDirReadsTheStandInFile(t *testing.T) {
	f, err := Dir("../../dev/tva").Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]bool{}
	nullOwner := false
	for _, s := range f.Systems {
		statuses[s.Status] = true
		if s.OwnerSubject == nil {
			nullOwner = true
		}
	}
	for _, want := range []string{StatusLive, StatusMayJoin, StatusPlanned, StatusInDevelopment, StatusRetired} {
		if !statuses[want] {
			t.Errorf("the stand-in has no %s", want)
		}
	}
	if !nullOwner {
		t.Error("the stand-in has no null owner subject")
	}
}

func TestDirFaults(t *testing.T) {
	if _, err := Dir(t.TempDir()).Read(context.Background()); err == nil {
		t.Fatal("no file was read as a file")
	}
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, FileName), []byte(`{"format":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Dir(d).Read(context.Background()); !errors.Is(err, ErrFormat) {
		t.Fatal(err)
	}
}
