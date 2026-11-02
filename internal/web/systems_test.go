package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/people"
	"github.com/Rumeasiyan/rosetta/internal/tva"
)

// ---- no database: who may act for a system, and what the screens send --------

func TestManagesFollowsTheSubjectAlone(t *testing.T) {
	sys := callers.Caller{OwnerSubject: "dev-owner"}
	admin := &current{grants: []people.Grant{{Role: people.Administrator}}}
	owner := &current{sess: session{subject: "dev-owner", email: "someone@example.invalid"}}
	other := &current{sess: session{subject: "dev-other", email: "dev-owner"}}
	keeper := &current{sess: session{subject: "dev-keeper"}, grants: []people.Grant{{Role: people.GlossaryKeeper}}}
	if !manages(admin, sys) || !manages(owner, sys) {
		t.Fatal("the administrator and the owner act for a system")
	}
	if manages(other, sys) || manages(keeper, sys) {
		t.Fatal("nobody else does, even with the subject as their email")
	}
	vacant := callers.Caller{}
	if manages(&current{}, vacant) || manages(other, vacant) {
		t.Fatal("no owner means nobody but the administrator, never someone with no subject")
	}
	if !manages(admin, vacant) {
		t.Fatal("the administrator acts for a system with no owner (R-CAL-02)")
	}
}

func TestSystemsScreenFollowsTheRole(t *testing.T) {
	admin := &current{grants: []people.Grant{{Role: people.Administrator}}}
	owner := &current{owned: []people.OwnedSystem{{Reference: "TEST-0001"}}}
	if !seesSystems(admin) || !seesSystems(owner) || seesSystems(&current{}) {
		t.Fatal("seesSystems")
	}
	has := func(c *current, id string) bool {
		for _, n := range nav(c) {
			if n.ID == id {
				return true
			}
		}
		return false
	}
	if !has(admin, "systems") || !has(owner, "systems") || has(&current{}, "systems") || has(owner, "people") {
		t.Fatal("the bar offers the wrong screens")
	}
}

func someSystems() []callers.Caller {
	at := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	return []callers.Caller{
		{ID: 7, Reference: "TEST-0001", Name: "Test letter editor", OwnerPostRef: "31", OwnerSubject: "dev-owner", TVAStatus: tva.StatusLive, Flag: callers.FlagNone, LastRequestAt: &at, PerMinute: 60, AtOnce: 5},
		{ID: 8, Reference: "TEST-0004", Name: "Test system with no owner", TVAStatus: tva.StatusMayJoin, Flag: callers.FlagNoOwner, PerMinute: 60, AtOnce: 5},
	}
}

func TestSystemsScreensInEachLanguage(t *testing.T) {
	for _, l := range i18n.Langs {
		body := render(t, "systems", Page{Lang: l, Path: "/systems", Title: "systems", Here: "systems", CSRF: "form-token", Viewer: &Viewer{Name: "Test"},
			Data: systemsData{Systems: someSystems(), Register: true}})
		for _, want := range []string{i18n.T(l, "registerTitle"), i18n.T(l, "flagNoOwner"), `href="/systems/7"`, "TEST-0004", `name="csrf" value="form-token"`, `<label for="reference">`, "2030-01-01 10:00", `action="/systems/register"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		owner := render(t, "systems", Page{Lang: l, Path: "/systems", Title: "systems", Viewer: &Viewer{Name: "Test"}, Data: systemsData{Systems: someSystems()[:1]}})
		if strings.Contains(owner, "/systems/register") {
			t.Errorf("%s: an owner is offered registration", l)
		}
		one := render(t, "system", Page{Lang: l, Path: "/systems/8", Title: "systems", Viewer: &Viewer{Name: "Test"}, Data: systemData{System: someSystems()[1]}})
		if !strings.Contains(one, i18n.T(l, "flagNoOwner")) || !strings.Contains(one, i18n.T(l, "postNotKnown")) {
			t.Errorf("%s: %s", l, one)
		}
	}
}

// ---- with the database -------------------------------------------------------

func (w *world) freshSystems() {
	w.t.Helper()
	w.exec(`DELETE FROM usage_record`)
	w.exec(`DELETE FROM daily_total`)
	w.exec(`DELETE FROM memory_entry`)
	w.exec(`DELETE FROM alert`)
	w.exec(`DELETE FROM caller_origin`)
	w.exec(`DELETE FROM caller_key`)
	w.exec(`DELETE FROM caller`)
}

func TestRegisteringFollowsTheRegister(t *testing.T) {
	const marker = "CALLERTEXTMARKER"
	w := newWorld(t)
	w.app.deps.TVA = tva.Dir("../../dev/tva")
	admin, who := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	w.exec(`DELETE FROM audit_outbox`)
	token := admin.csrf()

	reg := func(ref string) page {
		return admin.post("/systems/register", url.Values{"csrf": {token}, "reference": {ref}})
	}
	if p := reg("TEST-0001"); !strings.Contains(p.body, "has been registered") || !strings.Contains(p.body, "Test letter editor") {
		t.Fatalf("live: %d %s", p.status, p.body)
	}
	if p := reg("TEST-0002"); !strings.Contains(p.body, "has been registered") {
		t.Fatalf("may join: %d", p.status)
	}
	// A null owner subject is registered, and flagged.
	if p := reg("TEST-0003"); !strings.Contains(p.body, "flagged") {
		t.Fatalf("vacant: %s", p.body)
	}
	for _, ref := range []string{"TEST-0005", "TEST-0006", "TEST-0007"} {
		if p := reg(ref); !strings.Contains(p.body, "neither live nor may join") {
			t.Errorf("%s: %s", ref, p.body)
		}
	}
	if p := reg(marker); !strings.Contains(p.body, "no such reference") || strings.Contains(p.body, marker) {
		t.Errorf("unknown: %s", p.body)
	}
	if p := reg("TEST-0001"); !strings.Contains(p.body, "registered already") {
		t.Errorf("again: %s", p.body)
	}
	var n int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM caller`).Scan(&n)
	if n != 3 {
		t.Fatalf("%d systems", n)
	}
	var flag, owner, status string
	if err := w.pool.QueryRow(context.Background(), `SELECT flag, COALESCE(owner_subject, ''), tva_status FROM caller WHERE tva_reference = 'TEST-0003'`).Scan(&flag, &owner, &status); err != nil {
		t.Fatal(err)
	}
	if flag != callers.FlagNoOwner || owner != "" || status != "live" {
		t.Fatal(flag, owner, status)
	}
	got := w.outbox()
	if len(got) != 3 || got[0].Action != "creation" || got[0].Actor != who || got[0].ObjectType != "calling system" || got[0].ObjectRef != "TEST-0001" {
		t.Fatalf("%+v", got)
	}
	for _, e := range got {
		if strings.Contains(e.Raw, marker) {
			t.Fatal("the outbox holds what was typed")
		}
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal("a log holds what was typed")
	}
}

// A register that cannot be read registers nothing, and says so.
func TestRegisteringWithoutTheFile(t *testing.T) {
	w := newWorld(t)
	w.app.deps.TVA = tva.Dir(t.TempDir())
	admin, _ := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	p := admin.post("/systems/register", url.Values{"csrf": {admin.csrf()}, "reference": {"TEST-0001"}})
	if !strings.Contains(p.body, "could not be read") {
		t.Fatalf("%d %s", p.status, p.body)
	}
}

// A system owner reaches their own system and no other; anyone else is denied.
func TestOwnerReachesOnlyTheirOwnSystems(t *testing.T) {
	w := newWorld(t)
	w.app.deps.TVA = tva.Dir("../../dev/tva")
	admin, _ := w.signedInAdmin()
	w.freshSystems()
	t.Cleanup(w.freshSystems)
	ownerSubject := subject("owner")
	var mine, theirs int64
	for _, ins := range []struct {
		ref, subject string
		id           *int64
	}{{"OWN-1", ownerSubject, &mine}, {"OTHER-1", "someone-else", &theirs}} {
		if err := w.pool.QueryRow(context.Background(), `INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at)
			VALUES ($1, $1, $2, 'live', now()) RETURNING id`, ins.ref, ins.subject).Scan(ins.id); err != nil {
			t.Fatal(err)
		}
	}
	owner := w.browser()
	owner.signIn(ownerSubject)
	if p := owner.get("/systems"); p.status != 200 || !strings.Contains(p.body, "OWN-1") || strings.Contains(p.body, "OTHER-1") || strings.Contains(p.body, "/systems/register") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if p := owner.get(sysPath(mine)); p.status != 200 {
		t.Fatalf("own: %d", p.status)
	}
	if p := owner.get(sysPath(theirs)); p.status != 403 {
		t.Fatalf("another's: %d", p.status)
	}
	if p := owner.get("/systems/999999999"); p.status != 403 {
		t.Fatalf("one that does not exist tells an owner nothing: %d", p.status)
	}
	if p := owner.post("/systems/register", url.Values{"csrf": {owner.csrf()}, "reference": {"TEST-0001"}}); p.status != 403 {
		t.Fatalf("registering: %d", p.status)
	}
	if p := admin.get(sysPath(theirs)); p.status != 200 {
		t.Fatalf("the administrator: %d", p.status)
	}
	if p := admin.get("/systems/999999999"); p.status != 404 {
		t.Fatalf("not there: %d", p.status)
	}
	nobody := w.browser()
	nobody.signIn(subject("officer"))
	if p := nobody.get("/systems"); p.status != 403 {
		t.Fatalf("no role: %d", p.status)
	}
}

func sysPath(id int64) string { return "/systems/" + itoa(id) }
