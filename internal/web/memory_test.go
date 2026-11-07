package web

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/memory"
)

// caller is a calling system to hold memory entries.
func (w *world) caller() int64 {
	w.t.Helper()
	var id int64
	if err := w.pool.QueryRow(context.Background(), `INSERT INTO caller (tva_reference, name, tva_status, tva_checked_at) VALUES ($1, 'Invented System', 'live', now()) RETURNING id`, subject("MEM")).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

// remembered adds an entry directly, as the calling system's call would have.
func (w *world) remembered(caller int64, from, to, source, approved, state string) int64 {
	w.t.Helper()
	var id int64
	if err := w.pool.QueryRow(context.Background(), `INSERT INTO memory_entry (caller_id, glossary_version, from_language, to_language, source_text, source_hash, approved_text, approver_post, approved_on, state)
		VALUES ($1, 0, $2, $3, $4, md5($4), $5, 'An invented post', '2020-01-01', $6) RETURNING id`, caller, from, to, source, approved, state).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) memoryState(id int64) (state, source, approved string) {
	w.t.Helper()
	if err := w.pool.QueryRow(context.Background(), `SELECT state, source_text, approved_text FROM memory_entry WHERE id = $1`, id).Scan(&state, &source, &approved); err != nil {
		w.t.Fatal(err)
	}
	return
}

func (b *browser) memoryAct(id int64, action string, form url.Values) page {
	b.w.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf", b.csrf())
	return b.post(fmt.Sprintf("/memory/%d/%s", id, action), form)
}

// R-MEM-04: a reviewer for the language confirms, corrects or removes what is marked, and each is
// recorded with no text. Only a reviewer confirms or corrects.
func TestAReviewerRechecksTheirOwnLanguage(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	c := w.caller()
	reviewer, _ := w.holding("reviewer", "reviewer ta")
	confirm := w.remembered(c, "en", "ta", "One.", "ஒன்று.", memory.StateRecheck)
	correct := w.remembered(c, "en", "ta", "Two on ⟦M1⟧.", "⟦M1⟧ இல் இரண்டு.", memory.StateRecheck)
	remove := w.remembered(c, "en", "ta", "Three.", "மூன்று.", memory.StateRecheck)
	other := w.remembered(c, "en", "si", "Four.", "හතර.", memory.StateRecheck)
	if p := reviewer.get("/memory"); p.status != 200 || !strings.Contains(p.body, "ஒன்று.") || strings.Contains(p.body, "හතර.") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	says(t, reviewer.memoryAct(confirm, "confirm", nil), i18n.EN, "memoryConfirmed")
	if s, _, _ := w.memoryState(confirm); s != memory.StateServed {
		t.Fatal(s)
	}
	says(t, reviewer.memoryAct(confirm, "confirm", nil), i18n.EN, "memoryNotRecheck")
	// A correction keeps every stand-in, and takes no number of its own.
	for _, bad := range []string{"", "   ", "⟦M1⟧", "lost the mark", "⟦M1⟧ ⟦M1⟧ twice", "⟦M1⟧ with ⟦M2⟧ extra", "⟦M1⟧ and 0771234567 too", "⟦M1⟧ card 123456789V"} {
		says(t, reviewer.memoryAct(correct, "correct", url.Values{"text": {bad}}), i18n.EN, "memoryBadText")
	}
	says(t, reviewer.memoryAct(correct, "correct", url.Values{"text": {"இரண்டு ⟦M1⟧ இல்."}}), i18n.EN, "memoryCorrected")
	if s, _, text := w.memoryState(correct); s != memory.StateServed || text != "இரண்டு ⟦M1⟧ இல்." {
		t.Fatalf("%s %q", s, text)
	}
	says(t, reviewer.memoryAct(remove, "remove", nil), i18n.EN, "memoryRemoved")
	if s, source, text := w.memoryState(remove); s != memory.StateRemoved || source != "" || text != "" {
		t.Fatalf("%s %q %q", s, source, text)
	}
	says(t, reviewer.memoryAct(remove, "remove", nil), i18n.EN, "memoryGone")
	// Another language's entry: refused, recorded, unchanged, on every action and to read.
	for _, action := range []string{"confirm", "correct", "remove"} {
		if p := reviewer.memoryAct(other, action, url.Values{"text": {"වෙනස්."}}); p.status != 403 {
			t.Errorf("%s: %d", action, p.status)
		}
	}
	if p := reviewer.get(fmt.Sprintf("/memory/%d", other)); p.status != 403 {
		t.Error(p.status)
	}
	if s, _, text := w.memoryState(other); s != memory.StateRecheck || text != "හතර." {
		t.Fatalf("%s %q", s, text)
	}
	got := map[string]int{}
	for _, x := range w.outbox() {
		got[x.Action]++
		if x.Action != "denial" && (x.Post != "not known" || x.ObjectType != "memory entry") {
			t.Errorf("%+v", x)
		}
		for _, word := range []string{"ஒன்று", "இரண்டு", "மூன்று", "හතර", "An invented post"} {
			if strings.Contains(x.Raw, word) {
				t.Errorf("in the outbox: %s", x.Raw)
			}
		}
	}
	if got["approval"] != 1 || got["change"] != 1 || got["deletion"] != 1 || got["denial"] != 4 {
		t.Fatalf("%v", got)
	}
}

// R-MEM-05: the administrator finds by text and removes, in every language, but is not a reviewer.
func TestTheAdministratorFindsAndRemoves(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	c := w.caller()
	admin, _ := w.holding("admin", "admin")
	si := w.remembered(c, "en", "si", "A wrong sentence about Walks.", "වැරදි වාක්‍යයක්.", memory.StateServed)
	ta := w.remembered(c, "ta", "en", "தவறான வாக்கியம்.", "A wrong approved Walks line.", memory.StateRecheck)
	gone := w.remembered(c, "en", "ta", "A gone Walks entry.", "பழையது.", memory.StateReplaced)
	p := admin.get("/memory?find=" + url.QueryEscape("WALKS"))
	if !strings.Contains(p.body, "A wrong sentence about Walks.") || !strings.Contains(p.body, "A wrong approved Walks line.") || strings.Contains(p.body, "A gone Walks entry.") {
		t.Fatal(p.body)
	}
	if p := admin.get("/memory?find=" + url.QueryEscape("தவறான")); !strings.Contains(p.body, "தவறான வாக்கியம்.") {
		t.Fatal(p.body)
	}
	if p := admin.get("/memory?find=%25"); strings.Contains(p.body, "A wrong sentence") {
		t.Fatal("a % found everything")
	}
	if p := admin.get("/memory?find=nothing-holds-this"); !strings.Contains(p.body, i18n.T(i18n.EN, "memoryNoneFound")) {
		t.Fatal(p.body)
	}
	says(t, admin.memoryAct(si, "remove", nil), i18n.EN, "memoryRemoved")
	if s, _, _ := w.memoryState(si); s != memory.StateRemoved {
		t.Fatal(s)
	}
	if p := admin.get(fmt.Sprintf("/memory/%d", si)); p.status != 200 || strings.Contains(p.body, "A wrong sentence") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	for _, action := range []string{"confirm", "correct"} {
		if p := admin.memoryAct(ta, action, url.Values{"text": {"changed"}}); p.status != 403 {
			t.Errorf("%s: %d", action, p.status)
		}
	}
	says(t, admin.memoryAct(gone, "remove", nil), i18n.EN, "memoryGone")
	if p := admin.memoryAct(999999999, "remove", nil); p.status != 404 {
		t.Error(p.status)
	}
	if p := admin.get("/memory/nonsense"); p.status != 404 {
		t.Error(p.status)
	}
}

// R-ADM-01: no role and the wrong role are refused; a form with no token changes nothing.
func TestEveryMemoryScreenChecksTheRole(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	c := w.caller()
	id := w.remembered(c, "en", "ta", "One.", "ஒன்று.", memory.StateRecheck)
	officer, _ := w.holding("officer")
	keeper, _ := w.holding("keeper", "keeper ta")
	reviewer, _ := w.holding("reviewer", "reviewer ta")
	for name, b := range map[string]*browser{"officer": officer, "keeper": keeper} {
		for _, path := range []string{"/memory", fmt.Sprintf("/memory/%d", id)} {
			if p := b.get(path); p.status != 403 {
				t.Errorf("%s %s: %d", name, path, p.status)
			}
		}
		for _, action := range []string{"confirm", "correct", "remove"} {
			if p := b.memoryAct(id, action, url.Values{"text": {"changed"}}); p.status != 403 {
				t.Errorf("%s %s: %d", name, action, p.status)
			}
		}
	}
	for _, action := range []string{"confirm", "correct", "remove"} {
		if p := reviewer.post(fmt.Sprintf("/memory/%d/%s", id, action), url.Values{"text": {"changed"}}); p.status != 403 {
			t.Errorf("no token, %s: %d", action, p.status)
		}
	}
	if s, _, text := w.memoryState(id); s != memory.StateRecheck || text != "ஒன்று." {
		t.Fatalf("%s %q", s, text)
	}
	if p := w.browser().get("/memory"); p.url != "/mjolnir/oauth2/auth" {
		t.Errorf("%d %s", p.status, p.url)
	}
}

// Found is what the role sees; words typed to find go nowhere.
func TestNoMemoryWordReachesALogOrTheOutbox(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	c := w.caller()
	const secret = "Memoryscreenmarker"
	reviewer, _ := w.holding("reviewer", "reviewer ta")
	id := w.remembered(c, "en", "ta", secret+" one.", secret+" ஒன்று.", memory.StateRecheck)
	reviewer.get("/memory?find=" + secret)
	reviewer.get(fmt.Sprintf("/memory/%d", id))
	reviewer.memoryAct(id, "correct", url.Values{"text": {secret + " corrected."}})
	reviewer.memoryAct(id, "remove", nil)
	for _, x := range w.outbox() {
		if strings.Contains(x.Raw, secret) {
			t.Errorf("in the outbox: %s", x.Raw)
		}
	}
	if strings.Contains(w.logs.String(), secret) {
		t.Errorf("in the logs: %s", w.logs.String())
	}
}

func TestMemoryScreensInEachLanguage(t *testing.T) {
	version := 3
	glossaryEntry := int64(9)
	e := memory.Entry{ID: 7, From: "en", To: "ta", Source: "One.", Approved: "ஒன்று.", ApproverPost: "An invented post", System: "Invented System", State: memory.StateRecheck, RecheckEntry: &glossaryEntry, RecheckVersion: &version}
	for _, l := range i18n.Langs {
		list := render(t, "memory", Page{Lang: l, Path: "/memory", Title: "memory", Here: "memory", CSRF: "form-token", Viewer: &Viewer{Name: "Test"}, Data: memoryScreenData{Find: "One", Searched: true, Found: []memory.Entry{e}, Rechecks: []memory.Entry{e}, Waiting: 1}})
		for _, want := range []string{`href="/memory/7"`, "ஒன்று.", i18n.T(l, "memoryRechecks"), i18n.T(l, "memoryFound"), `lang="ta"`} {
			if !strings.Contains(list, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		page := render(t, "memory-entry", Page{Lang: l, Path: "/memory/7", Title: "memory", Here: "memory", CSRF: "form-token", Viewer: &Viewer{Name: "Test"}, Data: memoryEntryData{Entry: e, Left: 4, MayReview: true}})
		for _, want := range []string{`action="/memory/7/confirm"`, `action="/memory/7/correct"`, `action="/memory/7/remove"`, `href="/glossary/9"`, "band-en", "band-ta", i18n.T(l, "memoryAsApproved"), "An invented post"} {
			if !strings.Contains(page, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		// An administrator sees no confirm or correct.
		page = render(t, "memory-entry", Page{Lang: l, Path: "/memory/7", Title: "memory", CSRF: "form-token", Viewer: &Viewer{Name: "Test"}, Data: memoryEntryData{Entry: e}})
		if strings.Contains(page, `/memory/7/confirm`) || !strings.Contains(page, `/memory/7/remove`) {
			t.Errorf("%s: an administrator's page", l)
		}
	}
}

// R-GLO-06 and R-REC-02: the third check publishes and marks at once. The keeper is told, the reviewer
// sees which entry sent it there, and the audit says how many, with no word of the term or the text.
func TestAPublicationMarksAndSaysSoWithNoWords(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	c := w.caller()
	const secret = "Markedtermword"
	k := w.keepers()
	reviewer, _ := w.holding("reviewer", "reviewer ta")
	id := w.remembered(c, "en", "ta", "The "+secret+" came.", "வந்தார்.", memory.StateServed)
	entry := k[i18n.EN].propose("post", secret, "පදය", "சொல்", "")
	k[i18n.EN].check(entry, i18n.EN, "")
	k[i18n.SI].check(entry, i18n.SI, "")
	says(t, k[i18n.TA].check(entry, i18n.TA, ""), i18n.EN, "glossaryPublishedMarks")
	if s, _, _ := w.memoryState(id); s != memory.StateRecheck {
		t.Fatal(s)
	}
	if p := reviewer.get(fmt.Sprintf("/memory/%d", id)); !strings.Contains(p.body, fmt.Sprintf(`href="/glossary/%d"`, entry)) {
		t.Fatal(p.body)
	}
	found := false
	for _, x := range w.outbox() {
		if strings.Contains(x.Raw, secret) {
			t.Errorf("in the outbox: %s", x.Raw)
		}
		if x.ObjectType == "glossary version" {
			found = x.Details["memory_marked"] == float64(1) && x.Details["cache_removed"] == float64(0)
		}
	}
	if !found {
		t.Error("the publication does not say what it marked")
	}
	if strings.Contains(w.logs.String(), secret) {
		t.Error("in the logs")
	}
	// A reviewer's confirmation serves it again, and takes the mark away.
	says(t, reviewer.memoryAct(id, "confirm", nil), i18n.EN, "memoryConfirmed")
	var mark *int64
	if err := w.pool.QueryRow(context.Background(), `SELECT recheck_glossary_entry_id FROM memory_entry WHERE id = $1`, id).Scan(&mark); err != nil || mark != nil {
		t.Fatalf("%v %v", mark, err)
	}
}
