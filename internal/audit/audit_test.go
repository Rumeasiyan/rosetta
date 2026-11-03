package audit

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func good() Event {
	return Event{Actor: Actor{PersonID: 1, Subject: "dev-admin", Name: "Test Administrator", Post: PostNotKnown},
		Action: ActionRoleChange, ObjectType: "person", ObjectRef: "dev-keeper", Result: ResultDone,
		Details: map[string]any{"change": "granted", "role": "glossary keeper", "language": "ta"}}
}

func TestBuildMakesPensievesShape(t *testing.T) {
	at := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	e, err := build(good(), "rosetta-1", at)
	if err != nil {
		t.Fatal(err)
	}
	if e.Post != "not known" || e.Actor != "dev-admin" || e.OccurredAt != "2030-01-01T04:30:00Z" || e.Action != "role change" {
		t.Fatalf("%+v", e)
	}
	sys := good()
	sys.Actor = System
	if e, err := build(sys, "rosetta-2", at); err != nil || e.Post != "system" || e.Actor != "rosetta" {
		t.Fatalf("%v %+v", err, e)
	}
}

func TestBuildRefusesWhatPensieveWouldRefuse(t *testing.T) {
	for name, change := range map[string]func(*Event){
		"blank post":     func(e *Event) { e.Actor.Post = " " },
		"no actor":       func(e *Event) { e.Actor.Subject = "" },
		"off the list":   func(e *Event) { e.Action = "guess" },
		"sign-in":        func(e *Event) { e.Action = "sign-in" },
		"no object":      func(e *Event) { e.ObjectRef = "" },
		"bad result":     func(e *Event) { e.Result = "maybe" },
		"a key by name":  func(e *Event) { e.Details = map[string]any{"api_key": "x"} },
		"a key by look":  func(e *Event) { e.Details = map[string]any{"a": []any{"Bearer abc"}} },
		"a key of ours":  func(e *Event) { e.Details = map[string]any{"a": "pv_abcdefgh"} },
		"a card by name": func(e *Event) { e.Details = map[string]any{"NIC": "x"} },
		"old card":       func(e *Event) { e.Details = map[string]any{"a": "123456789V"} },
		"new card":       func(e *Event) { e.Details = map[string]any{"a": map[string]any{"b": "200012345678"}} },
		"card as number": func(e *Event) { e.Details = map[string]any{"a": 200012345678} },
		"too big":        func(e *Event) { e.Details = map[string]any{"a": strings.Repeat("x", maxDetails)} },
	} {
		e := good()
		change(&e)
		if _, err := build(e, "rosetta-1", time.Now()); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}

// A refusal never repeats the value it refused.
func TestARefusalHoldsNoValue(t *testing.T) {
	err := CheckDetails(map[string]any{"a": "123456789V"})
	if err == nil || strings.Contains(err.Error(), "123456789") {
		t.Fatalf("%v", err)
	}
}

func TestLongValuesAreCutNotRefused(t *testing.T) {
	e := good()
	e.Actor.Name = strings.Repeat("න", 300)
	got, err := build(e, "rosetta-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(got.ActorName)) != 200 {
		t.Fatalf("%d", len([]rune(got.ActorName)))
	}
}

func TestVisitor(t *testing.T) {
	r := &http.Request{Header: http.Header{}, RemoteAddr: "192.0.2.1:5000"}
	if Visitor(r) != "192.0.2.1" {
		t.Fatal(Visitor(r))
	}
	r.Header.Set("CF-Connecting-IP", "198.51.100.9")
	if Visitor(r) != "198.51.100.9" {
		t.Fatal(Visitor(r))
	}
	r.Header.Set("CF-Connecting-IP", "not an address")
	if Visitor(r) != "192.0.2.1" {
		t.Fatal(Visitor(r))
	}
}
