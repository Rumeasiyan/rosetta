package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
)

func (b *browser) add(email string) page {
	b.w.t.Helper()
	return b.post("/people/add", url.Values{"csrf": {b.csrf()}, "email": {email}})
}

func (w *world) count(sql string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// A good MJOLNIR account nobody added: refused, with no session, no row and its token back.
func TestAPersonNotAddedIsRefusedAndLeavesNoRow(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	who := subject("stranger")
	b := w.browser()
	p := b.signInAs(who)
	if p.status != http.StatusForbidden || !strings.Contains(p.body, i18n.T(i18n.EN, "notAdded")) {
		t.Fatalf("%d", p.status)
	}
	if n := w.count(`SELECT count(*) FROM person WHERE mjolnir_subject = $1 OR email = $2`, who, who+"@example.invalid"); n != 0 {
		t.Fatalf("%d rows", n)
	}
	revoked := 0
	for _, r := range w.received("mjolnir") {
		if strings.HasSuffix(r.Path, "/oauth2/revoke") {
			revoked++
		}
	}
	if revoked != 1 {
		t.Fatalf("revoked %d", revoked)
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "denial" || got[0].ObjectRef != who || strings.Contains(got[0].Raw, "@example.invalid") {
		t.Fatalf("%+v", got)
	}
	// It stops at the page rather than going round.
	if p := b.get("/"); p.status != 200 || p.url != "/signin" {
		t.Fatalf("%d %s", p.status, p.url)
	}
}

// R-ADM-08: added by email in any case, linked at the first sign-in, told once, and kept by the
// subject from then on: a new email changes nothing, and another account with the old one gets nothing.
func TestAddedByEmailThenKeptByTheSubject(t *testing.T) {
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	w.control("hedwig", "normal", nil)
	before := len(w.received("hedwig"))
	who := subject("added")
	email := who + "@example.invalid"
	if p := admin.add(strings.ToUpper(email)); !strings.Contains(p.body, i18n.T(i18n.EN, "personAdded")) || !strings.Contains(p.body, i18n.T(i18n.EN, "addedWaiting")) {
		t.Fatalf("%d", p.status)
	}
	says(t, admin.add(email), i18n.EN, "alreadyAdded")
	w.notifier().Send(context.Background(), w.pool)
	notices, _ := w.noticesSent()
	if len(notices) != before+1 || notices[len(notices)-1].NoticeType != "rosetta_person_added" || len(notices[len(notices)-1].People) != 1 {
		t.Fatalf("%+v", notices)
	}
	if link := notices[len(notices)-1].Details["link"]; !strings.HasSuffix(link, glossary.NewPersonPath) {
		t.Fatalf("%q", link)
	}
	b := w.browser()
	if p := b.signInAs(who); p.status != 200 {
		t.Fatalf("%d", p.status)
	}
	id := w.personID(who)
	if n := w.count(`SELECT count(*) FROM person WHERE lower(email) = lower($1)`, email); n != 1 {
		t.Fatalf("%d rows", n)
	}
	w.exec(`UPDATE person SET email = 'moved@example.invalid' WHERE id = $1`, id)
	again := w.browser()
	if p := again.signInAs(who); p.status != 200 {
		t.Fatalf("again: %d", p.status)
	}
	w.exec(`UPDATE person SET email = $2 WHERE id = $1`, id, email)
	if p := w.browser().signInAs(subject("taker")); p.status != http.StatusForbidden {
		t.Fatalf("taker: %d", p.status)
	}
}

// A role granted before a first sign-in is theirs from that sign-in on.
func TestARoleCanWaitForTheFirstSignIn(t *testing.T) {
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	who := subject("early")
	admin.add(who + "@example.invalid")
	p := admin.post("/people/grant", url.Values{"csrf": {admin.csrf()}, "email": {who + "@example.invalid"}, "role": {"administrator"}})
	says(t, p, i18n.EN, "granted")
	b := w.browser()
	b.signInAs(who)
	if p := b.get("/people"); p.status != 200 {
		t.Fatalf("%d", p.status)
	}
	stranger := subject("nobody")
	p = admin.post("/people/grant", url.Values{"csrf": {admin.csrf()}, "email": {stranger + "@example.invalid"}, "role": {"administrator"}})
	says(t, p, i18n.EN, "noSuchPerson")
}

// Signed in before being added, with another email then: the account stays and the two become one.
func TestAnOldAccountJoinsItsWaitingRow(t *testing.T) {
	w := newWorld(t)
	admin, _ := w.signedInAdmin()
	who := subject("old")
	w.exec(`INSERT INTO person (mjolnir_subject, name, email) VALUES ($1, 'Older', 'older@example.invalid')`, who)
	admin.add(who + "@example.invalid")
	b := w.browser()
	if p := b.signInAs(who); p.status != 200 {
		t.Fatalf("%d", p.status)
	}
	if n := w.count(`SELECT count(*) FROM person WHERE lower(email) = lower($1)`, who+"@example.invalid"); n != 1 {
		t.Fatalf("%d rows", n)
	}
}

// Removing needs live roles ended first and takes effect on the next request.
func TestRemovingEndsTheSessionAtOnce(t *testing.T) {
	w := newWorld(t)
	admin, adminWho := w.signedInAdmin()
	says(t, admin.post(fmt.Sprintf("/people/added/%d/remove", w.personID(adminWho)), url.Values{"csrf": {admin.csrf()}}), i18n.EN, "holdsRoles")
	who := subject("leaving")
	b := w.browser()
	b.signIn(who)
	if p := b.get("/"); p.status != 200 {
		t.Fatalf("%d", p.status)
	}
	says(t, admin.post(fmt.Sprintf("/people/added/%d/remove", w.personID(who)), url.Values{"csrf": {admin.csrf()}}), i18n.EN, "personRemoved")
	if p := b.noFollow(mustGet(w.screens.URL + "/")); p.status != http.StatusForbidden {
		t.Fatalf("%d", p.status)
	}
	if p := w.browser().signInAs(who); p.status != http.StatusForbidden {
		t.Fatalf("again: %d", p.status)
	}
}

// R-ADM-01: a term needs a role, however it is reached.
func TestShowingATermToCorrectNeedsARole(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypePost, "Invented Registrar", "රෙජිස්ට්‍රාර්", "பதிவாளர்", "")
	w.publish(id, k)
	officer, _ := w.holding("officer")
	if p := officer.get(fmt.Sprintf("/glossary/propose?corrects=%d", id)); p.status != http.StatusForbidden || strings.Contains(p.body, "பதிவாளர்") {
		t.Fatalf("%d", p.status)
	}
	f := url.Values{"csrf": {officer.csrf()}, "replaces": {itoa(id)}, "type": {glossary.TypePost}, "text_ta": {"புதிய பதிவாளர்"}}
	if p := officer.post("/glossary/propose", f); p.status != http.StatusForbidden {
		t.Fatalf("%d", p.status)
	}
	p := officer.post("/glossary/propose", url.Values{"csrf": {officer.csrf()}, "type": {glossary.TypeTerm}, "text_en": {"invented registrar"}})
	says(t, p, i18n.EN, "glossaryDuplicate")
	if strings.Contains(p.body, "பதிவாளர்") {
		t.Fatal("a duplicate showed the term")
	}
}

func TestProposalsStopAtTheDaysLimit(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	officer, _ := w.holding("officer")
	send := func(n int) page {
		return officer.post("/glossary/propose", url.Values{"csrf": {officer.csrf()}, "type": {glossary.TypeTerm}, "text_en": {fmt.Sprintf("invented limit term %d", n)}})
	}
	for n := range 20 {
		if p := send(n); p.status != 200 || strings.Contains(p.body, i18n.T(i18n.EN, "glossaryTooMany")) {
			t.Fatalf("%d: %d", n, p.status)
		}
	}
	says(t, send(20), i18n.EN, "glossaryTooMany")
}

// The notice's link asks for the sign-up form, and nothing else does.
func TestTheNoticesLinkAsksMjolnirForSignUp(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := b.noFollow(mustGet(w.screens.URL + glossary.NewPersonPath))
	if p.status != http.StatusSeeOther || !strings.Contains(p.header.Get("Location"), "sign_up=1") {
		t.Fatalf("%d %q", p.status, p.header.Get("Location"))
	}
	if p := b.noFollow(mustGet(w.screens.URL + "/signin/start")); strings.Contains(p.header.Get("Location"), "sign_up") {
		t.Fatalf("%q", p.header.Get("Location"))
	}
	// A hold is for starting by itself: following a link is someone asking.
	req := mustGet(w.screens.URL + glossary.NewPersonPath)
	req.AddCookie(&http.Cookie{Name: HoldCookie, Value: "1"})
	if p := b.noFollow(req); p.status != http.StatusSeeOther {
		t.Fatalf("held: %d", p.status)
	}
}

// Anyone can reach the link, so what it can add to memory has an end.
func TestTheOpenLinkStopsWhenTooManyWait(t *testing.T) {
	w := newWorld(t)
	s := w.app.sessions
	s.mu.Lock()
	for n := range WaitingLimit {
		s.waiting[itoa(int64(n))] = pending{created: time.Now()}
	}
	s.mu.Unlock()
	if p := w.browser().noFollow(mustGet(w.screens.URL + glossary.NewPersonPath)); p.status != http.StatusServiceUnavailable {
		t.Fatalf("%d", p.status)
	}
}
