package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// fresh clears roles and the outbox: these tests count both.
func (w *world) fresh() {
	w.t.Helper()
	w.exec(`DELETE FROM audit_outbox`)
	w.exec(`DELETE FROM role_grant`)
}

// signedInAdmin makes a first administrator through the bootstrap setting.
func (w *world) signedInAdmin() (*browser, string) {
	w.t.Helper()
	w.fresh()
	who := subject("admin")
	w.app.deps.FirstAdmin = who
	b := w.browser()
	if p := b.signIn(who); p.status != 200 {
		w.t.Fatalf("%d", p.status)
	}
	w.app.deps.FirstAdmin = ""
	return b, who
}

func (w *world) personID(who string) int64 {
	w.t.Helper()
	var id int64
	if err := w.pool.QueryRow(context.Background(), `SELECT id FROM person WHERE mjolnir_subject = $1`, who).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// outboxEntry is one row, as PENSIEVE will receive it.
type outboxEntry struct {
	Action, Actor, Post, ObjectType, ObjectRef, Result, Reason string
	Details                                                    map[string]any
	Raw                                                        string
}

func (w *world) outbox() []outboxEntry {
	w.t.Helper()
	rows, err := w.pool.Query(context.Background(), `SELECT details::text FROM audit_outbox ORDER BY id`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxEntry
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			w.t.Fatal(err)
		}
		var e struct {
			Action     string         `json:"action"`
			Actor      string         `json:"actor"`
			Post       string         `json:"post"`
			ObjectType string         `json:"object_type"`
			ObjectRef  string         `json:"object_ref"`
			Result     string         `json:"result"`
			Reason     string         `json:"reason"`
			Details    map[string]any `json:"details"`
		}
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, outboxEntry{e.Action, e.Actor, e.Post, e.ObjectType, e.ObjectRef, e.Result, e.Reason, e.Details, raw})
	}
	return out
}

func TestBootstrapWorksOnlyWhileNoAdministratorExists(t *testing.T) {
	w := newWorld(t)
	b, who := w.signedInAdmin()
	if p := b.get("/people"); p.status != 200 {
		t.Fatalf("the first administrator: %d", p.status)
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "role change" || got[0].Actor != "rosetta" || got[0].Post != "system" || got[0].ObjectRef != who {
		t.Fatalf("%+v", got)
	}
	// Signing in again changes nothing.
	w.app.deps.FirstAdmin = who
	w.control("mjolnir", "form-always", nil)
	w.browser().signIn(who)
	var n int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM role_grant WHERE role = 'administrator'`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d administrators", n)
	}
	// While one exists, the setting makes nobody else an administrator.
	other := subject("late")
	w.app.deps.FirstAdmin = other
	late := w.browser()
	late.signIn(other)
	if p := late.get("/people"); p.status != 403 {
		t.Fatalf("a second bootstrap: %d", p.status)
	}
	// Someone the setting does not name gets nothing, with none in place either.
	w.fresh()
	w.app.deps.FirstAdmin = "somebody-else"
	nobody := w.browser()
	nobody.signIn(subject("nobody"))
	if p := nobody.get("/people"); p.status != 403 {
		t.Fatalf("not named: %d", p.status)
	}
}

func TestEveryScreenChecksTheRoleOnEveryRequest(t *testing.T) {
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	who := subject("keeper")
	keeper := w.browser()
	keeper.signIn(who)
	w.exec(`DELETE FROM audit_outbox`)
	// Without a role: refused, and the refusal is an entry naming the route.
	if p := keeper.get("/people"); p.status != 403 {
		t.Fatalf("%d", p.status)
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "denial" || got[0].Result != "denied" || got[0].Post != "not known" || got[0].Actor != who || got[0].ObjectRef != "GET /people" {
		t.Fatalf("%+v", got)
	}
	// A form from someone with no role is refused before it does anything.
	token := keeper.csrf()
	if p := keeper.post("/people/grant", url.Values{"csrf": {token}, "email": {who + "@example.invalid"}, "role": {"administrator"}}); p.status != 403 {
		t.Fatalf("%d", p.status)
	}
	// Granted: the next request has it. Ended: the next request has lost it.
	adminToken := admin.csrf()
	id := w.personID(who)
	if p := admin.post("/people/grant", url.Values{"csrf": {adminToken}, "email": {strings.ToUpper(who) + "@EXAMPLE.INVALID"}, "role": {"administrator"}}); p.status != 200 || !strings.Contains(p.body, "granted") && !strings.Contains(p.body, "The role has been granted") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if p := keeper.get("/people"); p.status != 200 {
		t.Fatalf("after the grant: %d", p.status)
	}
	var grant int64
	w.pool.QueryRow(context.Background(), `SELECT id FROM role_grant WHERE person_id = $1 AND ended_at IS NULL`, id).Scan(&grant)
	if p := admin.post(fmt.Sprintf("/people/%d/end", grant), url.Values{"csrf": {adminToken}}); p.status != 200 {
		t.Fatalf("%d", p.status)
	}
	if p := keeper.get("/people"); p.status != 403 {
		t.Fatalf("an ended role still opens the screen: %d", p.status)
	}
	var endedBy *int64
	w.pool.QueryRow(context.Background(), `SELECT ended_by FROM role_grant WHERE id = $1`, grant).Scan(&endedBy)
	if endedBy == nil {
		t.Fatal("ended_by not kept")
	}
	// Both changes are entries, with who and what.
	changes := 0
	for _, e := range w.outbox() {
		if e.Action == "role change" {
			changes++
			if e.Post != "not known" || e.ObjectRef != who {
				t.Errorf("%+v", e)
			}
		}
	}
	if changes != 2 {
		t.Fatalf("%d role changes", changes)
	}
}

func TestARoleWithAnEndDateEndsAfterIt(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	who := subject("dated")
	b := w.browser()
	b.signIn(who)
	id := w.personID(who)
	w.exec(`INSERT INTO role_grant (person_id, role, ends_on) VALUES ($1, 'administrator', (now() AT TIME ZONE 'Asia/Colombo')::date)`, id)
	if p := b.get("/people"); p.status != 200 {
		t.Fatalf("on its last day: %d", p.status)
	}
	w.exec(`UPDATE role_grant SET ends_on = ends_on - 1 WHERE person_id = $1`, id)
	if p := b.get("/people"); p.status != 403 {
		t.Fatalf("after its last day: %d", p.status)
	}
}

func TestGrantAndEndRefusals(t *testing.T) {
	w := newWorld(t)
	admin, who := w.signedInAdmin()
	token := admin.csrf()
	me := who + "@example.invalid"
	for name, c := range map[string]struct {
		form url.Values
		word string
	}{
		"system owner":        {url.Values{"email": {me}, "role": {"system owner"}}, "That role cannot be granted here."},
		"administrator again": {url.Values{"email": {me}, "role": {"administrator"}}, "That person holds this role already."},
		"language wrong":      {url.Values{"email": {me}, "role": {"administrator"}, "language": {"ta"}}, "An administrator has none."},
		"language missing":    {url.Values{"email": {me}, "role": {"glossary keeper"}}, "needs one language"},
		"past date":           {url.Values{"email": {me}, "role": {"glossary keeper"}, "language": {"ta"}, "ends_on": {"2001-01-01"}}, "from today on"},
		"no such person":      {url.Values{"email": {"nobody@example.invalid"}, "role": {"administrator"}}, "has not signed in"},
		"no email":            {url.Values{"role": {"administrator"}}, "has not signed in"},
	} {
		c.form.Set("csrf", token)
		before := len(w.outbox())
		p := admin.post("/people/grant", c.form)
		if p.status != 200 || !strings.Contains(p.body, c.word) {
			t.Errorf("%s: %d, no %q", name, p.status, c.word)
		}
		if after := len(w.outbox()); after != before {
			t.Errorf("%s: a refused change wrote an entry", name)
		}
	}
	// A form without its token is refused, and that refusal is an entry.
	before := len(w.outbox())
	if p := admin.post("/people/grant", url.Values{"email": {me}, "role": {"glossary keeper"}, "language": {"ta"}}); p.status != 403 {
		t.Fatalf("%d", p.status)
	}
	if got := w.outbox(); len(got) != before+1 || got[before].Action != "denial" {
		t.Fatalf("%+v", got)
	}
	// The last administrator stays.
	var grant int64
	w.pool.QueryRow(context.Background(), `SELECT id FROM role_grant WHERE person_id = $1 AND ended_at IS NULL`, w.personID(who)).Scan(&grant)
	if p := admin.post(fmt.Sprintf("/people/%d/end", grant), url.Values{"csrf": {token}}); !strings.Contains(p.body, "last administrator") {
		t.Fatalf("%d", p.status)
	}
	if p := admin.get("/people"); p.status != 200 {
		t.Fatalf("%d", p.status)
	}
	if p := admin.post("/people/not-a-number/end", url.Values{"csrf": {token}}); !strings.Contains(p.body, "no longer held") {
		t.Fatalf("%d", p.status)
	}
}

// What a person types into a form reaches no log, record, entry or answer.
func TestFormTextReachesNothing(t *testing.T) {
	const marker = "CALLERTEXTMARKER"
	w := newWorld(t)
	admin, who := w.signedInAdmin()
	token := admin.csrf()
	me := who + "@example.invalid"
	for _, form := range []url.Values{
		{"email": {marker}, "role": {"administrator"}},
		{"email": {me}, "role": {marker}},
		{"email": {me}, "role": {"glossary keeper"}, "language": {marker}},
		{"email": {me}, "role": {"glossary keeper"}, "language": {"ta"}, "ends_on": {marker}},
	} {
		form.Set("csrf", token)
		if p := admin.post("/people/grant", form); strings.Contains(p.body, marker) {
			t.Fatalf("an answer repeats it: %v", form)
		}
	}
	if p := admin.get("/people?m=" + marker); strings.Contains(p.body, marker) {
		t.Fatal("a flash shows it")
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal("a log holds it")
	}
	for _, e := range w.outbox() {
		if strings.Contains(e.Raw, marker) {
			t.Fatalf("an entry holds it: %s", e.Raw)
		}
	}
	var n int
	w.pool.QueryRow(context.Background(), `SELECT count(*) FROM role_grant WHERE role = $1 OR language = $1 OR post_ref = $1`, marker).Scan(&n)
	if n != 0 {
		t.Fatal("a record holds it")
	}
}

// A system owner comes from TVA's copy, and sees their own systems and no others.
func TestSystemOwnerSeesOnlyTheirOwn(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	who := subject("owner")
	ref, other := "TEST-"+who, "TEST-other-"+who
	for _, row := range [][2]string{{ref, who}, {other, "somebody-else"}} {
		w.exec(`INSERT INTO caller (tva_reference, name, owner_subject, tva_status, tva_checked_at) VALUES ($1, $1, $2, 'live', now())`, row[0], row[1])
	}
	t.Cleanup(func() {
		w.pool.Exec(context.Background(), `DELETE FROM caller WHERE tva_reference = ANY($1)`, []string{ref, other})
	})
	b := w.browser()
	b.signIn(who)
	p := b.get("/")
	if !strings.Contains(p.body, ref) || strings.Contains(p.body, other) {
		t.Fatalf("%s", p.body)
	}
	if !strings.Contains(p.body, "System owner") {
		t.Fatal("the role is not shown")
	}
	if p := b.get("/people"); p.status != 403 {
		t.Fatalf("an owner on the administrator's screen: %d", p.status)
	}
}

func TestLanguageIsChosenAndRemembered(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	who := subject("speaker")
	b := w.browser()
	b.signIn(who)
	if p := b.get("/lang/ta?next=/"); p.status != 200 || !strings.Contains(p.body, `<html lang="ta">`) {
		t.Fatalf("%d", p.status)
	}
	var stored string
	w.pool.QueryRow(context.Background(), `SELECT screen_language FROM person WHERE mjolnir_subject = $1`, who).Scan(&stored)
	if stored != "ta" {
		t.Fatalf("%q", stored)
	}
	// Another browser, no cookie: the stored choice follows the person.
	w.control("mjolnir", "form-always", nil)
	other := w.browser()
	if p := other.signIn(who); !strings.Contains(p.body, `<html lang="ta">`) {
		t.Fatalf("%s", p.body)
	}
	if p := b.get("/lang/fr"); p.status != 404 {
		t.Fatalf("%d", p.status)
	}
	p := b.noFollow(mustGet(w.screens.URL + "/lang/si?next=//evil.example"))
	if p.status != http.StatusSeeOther || p.header.Get("Location") != "/" {
		t.Fatalf("%d %v", p.status, p.header)
	}
}
