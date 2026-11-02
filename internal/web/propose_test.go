package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/hedwig"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/standin"
)

func (w *world) notifier() *glossary.Notifier {
	return &glossary.Notifier{Pool: w.pool, PublicURL: "https://rosetta.example.invalid", Sender: hedwig.Client{URL: w.standin.URL + "/hedwig", Key: standin.HedwigTestKey}}
}

// noticeSent is one call as HEDWIG's stand-in saw it.
type noticeSent struct {
	NoticeType string            `json:"notice_type"`
	RequestKey string            `json:"request_key"`
	Details    map[string]string `json:"details"`
	People     []hedwig.Person   `json:"people"`
}

func (w *world) noticesSent() ([]noticeSent, string) {
	w.t.Helper()
	var out []noticeSent
	var raw strings.Builder
	for _, r := range w.received("hedwig") {
		var n noticeSent
		if err := json.Unmarshal([]byte(r.Body), &n); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, n)
		raw.WriteString(r.Body)
	}
	return out, raw.String()
}

func (w *world) unsent() int {
	w.t.Helper()
	var n int
	if err := w.pool.QueryRow(context.Background(), `SELECT count(*) FROM alert WHERE sent_at IS NULL`).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// R-GLO-04: any signed-in officer may propose, with no role at all; the keepers for the languages
// concerned are told through HEDWIG's stand-in, by an opaque reference and in their own language;
// the proposer sees the outcome on screen.
func TestAnOfficerProposesKeepersAreToldAndTheOutcomeShows(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	w.exec(`UPDATE person SET screen_language = 'ta' WHERE id IN (SELECT person_id FROM role_grant WHERE language = 'ta')`)
	officer, who := w.holding("officer")
	if p := officer.get("/glossary/propose"); p.status != 200 {
		t.Fatal(p.status)
	}
	id := officer.propose(glossary.TypeTerm, "Invented Board", "", "", "")
	if p := officer.get(fmt.Sprintf("/glossary/%d", id)); p.status != 200 || !strings.Contains(p.body, "Invented Board") {
		t.Fatalf("their own entry: %d", p.status)
	}
	if p := officer.get("/glossary/mine"); !strings.Contains(p.body, "Invented Board") || !strings.Contains(p.body, i18n.T(i18n.EN, "glossaryOutcomeWaiting")) {
		t.Fatal(p.body)
	}
	// Told, and told once however many times it is sent.
	n := w.notifier()
	if sent, err := n.Send(context.Background(), w.pool); err != nil || sent != 1 {
		t.Fatalf("%d %v", sent, err)
	}
	if sent, err := n.Send(context.Background(), w.pool); err != nil || sent != 0 {
		t.Fatalf("%d %v", sent, err)
	}
	notices, raw := w.noticesSent()
	if len(notices) != 1 || notices[0].NoticeType != "glossary_proposal" || len(notices[0].People) != 3 {
		t.Fatalf("%+v", notices)
	}
	seenTamil := false
	for _, p := range notices[0].People {
		if !strings.HasPrefix(p.Ref, "rosetta-person-") || !strings.Contains(p.Email, "@") {
			t.Errorf("%+v", p)
		}
		seenTamil = seenTamil || p.Language == "ta"
		// The reference says nothing of who: not their account, and no address.
		if strings.Contains(p.Ref, who) || strings.Contains(p.Ref, "@") {
			t.Errorf("%+v", p)
		}
	}
	if !seenTamil {
		t.Error("nobody told in their own language")
	}
	if strings.Contains(raw, "Invented Board") {
		t.Fatalf("a term in a notice: %s", raw)
	}
	if notices[0].Details["link"] != fmt.Sprintf("https://rosetta.example.invalid/glossary/%d", id) {
		t.Fatalf("%+v", notices[0].Details)
	}
	// The outcome, on screen: a refusal with why, and a publication with its version.
	k[i18n.TA].post(fmt.Sprintf("/glossary/%d/refuse", id), url.Values{"csrf": {k[i18n.TA].csrf()}, "language": {"ta"}, "note": {"An invented reason."}})
	if p := officer.get("/glossary/mine"); !strings.Contains(p.body, "An invented reason.") || !strings.Contains(p.body, i18n.T(i18n.EN, "statusRefused")) {
		t.Fatal(p.body)
	}
	second := officer.propose(glossary.TypeTerm, "Another Invented Board", "මණ්ඩලය", "சபை", "")
	w.publish(second, k)
	if p := officer.get("/glossary/mine"); !strings.Contains(p.body, i18n.T(i18n.EN, "glossaryInVersion")) {
		t.Fatal(p.body)
	}
	// What is not theirs stays closed, and a keeper's action stays a keeper's.
	otherID := k[i18n.EN].propose(glossary.TypeTerm, "A Third Board", "", "", "")
	if p := officer.get(fmt.Sprintf("/glossary/%d", otherID)); p.status != 403 {
		t.Fatal(p.status)
	}
	if p := officer.check(second, i18n.EN, "changed"); p.status != 403 {
		t.Fatal(p.status)
	}
	if p := officer.post(fmt.Sprintf("/glossary/%d/refuse", otherID), url.Values{"csrf": {officer.csrf()}, "language": {"en"}, "note": {"no"}}); p.status != 403 {
		t.Fatal(p.status)
	}
	if p := officer.get("/glossary"); p.status != 403 {
		t.Fatal(p.status)
	}
}

// R-GLO-04: only the keepers for the languages concerned: a correction of the Tamil asks the
// Tamil keeper alone.
func TestOnlyTheKeepersConcernedAreTold(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypePost, "Invented Clerk", "ලිපිකරු", "எழுத்தர்", "")
	w.publish(id, k)
	n := w.notifier()
	n.Send(context.Background(), w.pool)
	w.control("hedwig", "normal", nil)
	before := len(w.received("hedwig"))
	officer, _ := w.holding("officer")
	f := url.Values{"csrf": {officer.csrf()}, "replaces": {itoa(id)}, "type": {glossary.TypePost}, "text_ta": {"தலைமை எழுத்தர்"}}
	if p := officer.post("/glossary/propose", f); p.status != 200 || !strings.Contains(p.url, "/glossary/") {
		t.Fatalf("%d %s", p.status, p.url)
	}
	if sent, err := n.Send(context.Background(), w.pool); err != nil || sent != 1 {
		t.Fatalf("%d %v", sent, err)
	}
	notices, _ := w.noticesSent()
	last := notices[len(notices)-1]
	if len(notices) != before+1 || len(last.People) != 1 || last.Details["languages"] != "ta" {
		t.Fatalf("%+v", last)
	}
}

// A term someone has published already is refused, and takes the officer to that entry's correction
// instead, with its words filled in.
func TestAProposalOfAPublishedTermLeadsToItsCorrection(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypePost, "Invented Clerk", "ලිපිකරු", "எழுத்தர்", "")
	w.publish(id, k)
	officer, _ := w.holding("officer")
	p := officer.post("/glossary/propose", url.Values{"csrf": {officer.csrf()}, "type": {glossary.TypeTerm}, "text_en": {"invented clerk"}})
	if !strings.Contains(p.body, fmt.Sprintf(`name="replaces" value="%d"`, id)) || !strings.Contains(p.body, "எழுத்தர்") || !strings.Contains(p.body, i18n.T(i18n.EN, "glossaryDuplicate")) {
		t.Fatal(p.body)
	}
	// Only a published entry can be corrected.
	waiting := k[i18n.EN].propose(glossary.TypeTerm, "Waiting Term", "", "", "")
	says(t, officer.get(fmt.Sprintf("/glossary/propose?corrects=%d", waiting)), i18n.EN, "glossaryNotCurrent")
	if p := officer.get("/glossary/propose?corrects=999999"); p.status != 404 {
		t.Fatal(p.status)
	}
}

// HEDWIG down or refusing costs nobody their proposal: the notice waits, and the catch-up sends it
// once. When HEDWIG asks for a wait the rest wait with it.
func TestANoticeThatCannotGoWaits(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	w.keepers()
	officer, _ := w.holding("officer")
	officer.propose(glossary.TypeTerm, "First Invented", "", "", "")
	officer.propose(glossary.TypeTerm, "Second Invented", "", "", "")
	n := w.notifier()
	ctx := context.Background()
	for _, mode := range []string{"internal", "unknown_notice_type", "rate_limited", "unauthorized"} {
		w.control("hedwig", mode, nil)
		if sent, err := n.Send(ctx, w.pool); err != nil || sent != 0 || w.unsent() != 2 {
			t.Fatalf("%s: %d %v %d", mode, sent, err, w.unsent())
		}
	}
	w.control("hedwig", "normal", nil)
	if sent, err := n.Send(ctx, w.pool); err != nil || sent != 2 || w.unsent() != 0 {
		t.Fatalf("%d %v %d", sent, err, w.unsent())
	}
	var kept string
	if err := w.pool.QueryRow(ctx, `SELECT hedwig_notice_id FROM alert ORDER BY id LIMIT 1`).Scan(&kept); err != nil || kept == "" {
		t.Fatalf("%q %v", kept, err)
	}
	if strings.Contains(w.logs.String(), "Invented") {
		t.Fatal(w.logs.String())
	}
}

// With no keeper for any language nobody can be told: it is not retried for ever, and the work stays
// on the waiting list. A notice for something already settled tells nobody.
func TestANoticeWithNobodyToTell(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	officer, _ := w.holding("officer")
	id := officer.propose(glossary.TypeTerm, "Lonely Invented", "", "", "")
	n := w.notifier()
	ctx := context.Background()
	if sent, err := n.Send(ctx, w.pool); err != nil || sent != 0 || w.unsent() != 0 {
		t.Fatalf("%d %v %d", sent, err, w.unsent())
	}
	if len(w.received("hedwig")) != 0 {
		t.Fatal("told someone")
	}
	admin, _ := w.holding("admin", "admin")
	if p := admin.get("/glossary/waiting"); !strings.Contains(p.body, "Lonely Invented") {
		t.Fatalf("%d", id)
	}
	k := w.keepers()
	settled := officer.propose(glossary.TypeTerm, "Settled Invented", "", "", "")
	k[i18n.TA].post(fmt.Sprintf("/glossary/%d/refuse", settled), url.Values{"csrf": {k[i18n.TA].csrf()}, "language": {"ta"}, "note": {"An invented reason."}})
	if sent, err := n.Send(ctx, w.pool); err != nil || sent != 0 || w.unsent() != 0 {
		t.Fatalf("%d %v %d", sent, err, w.unsent())
	}
}

// Nothing of a proposal reaches a log, the outbox or HEDWIG; the outbox says what was done.
func TestNoProposalWordLeaves(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	w.keepers()
	const secret = "Markerproposalword"
	officer, _ := w.holding("officer")
	officer.propose(glossary.TypeTerm, secret, "", "", secret+" note")
	w.notifier().Send(context.Background(), w.pool)
	found := false
	for _, x := range w.outbox() {
		if strings.Contains(x.Raw, secret) {
			t.Errorf("in the outbox: %s", x.Raw)
		}
		found = found || (x.Action == "creation" && x.ObjectType == "glossary entry" && x.Post == "not known")
	}
	if !found {
		t.Error("no entry for the proposal")
	}
	if _, raw := w.noticesSent(); strings.Contains(raw, secret) {
		t.Errorf("in a notice: %s", raw)
	}
	if strings.Contains(w.logs.String(), secret) {
		t.Error("in the logs")
	}
	if p := officer.get("/"); !strings.Contains(p.body, `href="/glossary/propose"`) || !strings.Contains(p.body, `href="/glossary/mine"`) {
		t.Fatal(p.body)
	}
}
