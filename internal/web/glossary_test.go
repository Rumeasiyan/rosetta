package web

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
)

// glossaryClean empties what these tests count. Entries are never deleted in the service: a
// test alone may do so, since each one starts from an empty glossary.
func (w *world) glossaryClean() {
	w.t.Helper()
	w.exec(`DELETE FROM alert`)
	w.exec(`DELETE FROM cache_piece`)
	w.exec(`DELETE FROM memory_entry`)
	w.exec(`DELETE FROM glossary_term`)
	w.exec(`DELETE FROM glossary_entry`)
	w.exec(`DELETE FROM glossary_import`)
	w.exec(`DELETE FROM glossary_version WHERE number > 0`)
}

// holding signs a new person in with these roles: "keeper si", "reviewer ta", "admin".
func (w *world) holding(prefix string, roles ...string) (*browser, string) {
	w.t.Helper()
	who := subject(prefix)
	b := w.browser()
	if p := b.signIn(who); p.status != 200 {
		w.t.Fatalf("%d", p.status)
	}
	w.exec(`UPDATE person SET email = $2 WHERE mjolnir_subject = $1`, who, who+"@example.invalid")
	id := w.personID(who)
	for _, r := range roles {
		kind, lang, _ := strings.Cut(r, " ")
		switch kind {
		case "admin":
			w.exec(`INSERT INTO role_grant (person_id, role) VALUES ($1, 'administrator')`, id)
		case "keeper":
			w.exec(`INSERT INTO role_grant (person_id, role, language) VALUES ($1, 'glossary keeper', $2)`, id, lang)
		case "reviewer":
			w.exec(`INSERT INTO role_grant (person_id, role, language) VALUES ($1, 'memory reviewer', $2)`, id, lang)
		}
	}
	return b, who
}

// keepers is one keeper for each language.
func (w *world) keepers() map[i18n.Lang]*browser {
	out := map[i18n.Lang]*browser{}
	for _, l := range i18n.Langs {
		out[l], _ = w.holding("keeper-"+string(l), "keeper "+string(l))
	}
	return out
}

var entryPath = regexp.MustCompile(`^/glossary/(\d+)$`)

// propose sends the form and returns the new entry's number.
func (b *browser) propose(kind, en, si, ta, note string) int64 {
	b.w.t.Helper()
	p := b.post("/glossary/propose", url.Values{"csrf": {b.csrf()}, "type": {kind}, "text_en": {en}, "text_si": {si}, "text_ta": {ta}, "note": {note}})
	m := entryPath.FindStringSubmatch(p.url)
	if p.status != 200 || m == nil {
		b.w.t.Fatalf("%d at %s: %s", p.status, p.url, p.body)
	}
	var id int64
	fmt.Sscan(m[1], &id)
	return id
}

func (b *browser) check(id int64, l i18n.Lang, text string) page {
	b.w.t.Helper()
	return b.post(fmt.Sprintf("/glossary/%d/check", id), url.Values{"csrf": {b.csrf()}, "language": {string(l)}, "text": {text}})
}

// publish has all three keepers check an entry.
func (w *world) publish(id int64, k map[i18n.Lang]*browser) {
	w.t.Helper()
	for _, l := range i18n.Langs {
		if p := k[l].check(id, l, ""); p.status != 200 || strings.Contains(p.body, `note bad`) {
			w.t.Fatalf("%s: %d %s", l, p.status, p.body)
		}
	}
}

func (w *world) entry(id int64) glossary.Entry {
	w.t.Helper()
	e, err := glossary.Get(context.Background(), w.pool, id)
	if err != nil {
		w.t.Fatal(err)
	}
	return e
}

func (w *world) version() int {
	w.t.Helper()
	n, err := glossary.Current(context.Background(), w.pool)
	if err != nil {
		w.t.Fatal(err)
	}
	return n
}

// says looks for a message in English, as these browsers ask for no other language.
func says(t *testing.T, p page, _ i18n.Lang, word string) {
	t.Helper()
	if !strings.Contains(p.body, i18n.T(i18n.EN, word)) {
		t.Errorf("does not say %s: %d %s", word, p.status, p.body)
	}
}

// R-GLO-02: published only when all three are checked; two checked is held. R-GLO-05: the
// version goes up by one.
func TestAnEntryIsPublishedOnlyWhenAllThreeAreChecked(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypePost, "Chief Secretary", "ප්‍රධාන ලේකම්", "பிரதம செயலாளர்", "An invented note.")
	if e := w.entry(id); e.Status != glossary.StatusProposed || len(e.Unchecked()) != 3 {
		t.Fatalf("%+v", e)
	}
	before := w.version()
	k[i18n.EN].check(id, i18n.EN, "")
	if e := w.entry(id); e.Status != glossary.StatusHeld {
		t.Fatal(e.Status)
	}
	k[i18n.SI].check(id, i18n.SI, "")
	if e := w.entry(id); e.Status != glossary.StatusHeld || w.version() != before {
		t.Fatalf("two checked: %s, version %d", e.Status, w.version())
	}
	p := k[i18n.TA].check(id, i18n.TA, "")
	e := w.entry(id)
	if e.Status != glossary.StatusPublished || w.version() != before+1 || e.PublishedIn == nil || *e.PublishedIn != before+1 {
		t.Fatalf("%+v version %d", e, w.version())
	}
	says(t, p, i18n.TA, "glossaryPublished")
	for _, term := range e.Terms {
		if term.CheckedAt == nil || term.CheckedBy == "" {
			t.Errorf("%+v", term)
		}
	}
	// A published entry is not for keepers any more.
	says(t, k[i18n.TA].check(id, i18n.TA, "வேறு"), i18n.TA, "glossaryNotWaiting")
}

// A keeper's correction is what is checked, and a keeper may write a language left blank.
func TestAKeeperCorrectsTheirOwnLanguage(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypeTerm, "Gazette", "", "", "")
	says(t, k[i18n.TA].check(id, i18n.TA, ""), i18n.TA, "glossaryNoText")
	k[i18n.TA].check(id, i18n.TA, "வர்த்தமானி")
	k[i18n.EN].check(id, i18n.EN, "The Gazette")
	if e := w.entry(id); e.Term(i18n.TA).Text != "வர்த்தமானி" || e.Term(i18n.EN).Text != "The Gazette" || e.Status != glossary.StatusHeld {
		t.Fatalf("%+v", e)
	}
	says(t, k[i18n.EN].check(id, i18n.EN, ""), i18n.EN, "glossaryAlreadyChecked")
}

// s2: a keeper acts only on their own language, and an attempt is a denial.
func TestAKeeperActsOnlyOnTheirOwnLanguage(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypeTerm, "Gazette", "ගැසට්", "வர்த்தமானி", "")
	for _, l := range []i18n.Lang{i18n.SI, i18n.TA} {
		if p := k[i18n.EN].check(id, l, ""); p.status != 403 {
			t.Errorf("%s: %d", l, p.status)
		}
		p := k[i18n.EN].post(fmt.Sprintf("/glossary/%d/refuse", id), url.Values{"csrf": {k[i18n.EN].csrf()}, "language": {string(l)}, "note": {"no"}})
		if p.status != 403 {
			t.Errorf("%s: %d", l, p.status)
		}
	}
	if e := w.entry(id); e.Status != glossary.StatusProposed {
		t.Fatal(e.Status)
	}
	denied := 0
	for _, x := range w.outbox() {
		if x.Action == "denial" && x.ObjectRef != "" {
			denied++
		}
	}
	if denied != 4 {
		t.Fatalf("%d denials", denied)
	}
}

// R-ADM-01: without a role the list, a term and the versions are refused; with any role they
// open. Only a keeper checks: an administrator is refused.
func TestEveryGlossaryScreenChecksTheRole(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypeTerm, "Gazette", "ගැසට්", "வர்த்தமானி", "")
	w.publish(id, k)
	nobody, _ := w.holding("officer")
	admin, _ := w.holding("admin", "admin")
	reviewer, _ := w.holding("reviewer", "reviewer si")
	paths := []string{"/glossary", fmt.Sprintf("/glossary/%d", id), fmt.Sprintf("/glossary/%d/history", id), "/glossary-versions", "/glossary-versions/1"}
	for _, path := range paths {
		if p := nobody.get(path); p.status != 403 {
			t.Errorf("no role, %s: %d", path, p.status)
		}
		for name, b := range map[string]*browser{"admin": admin, "reviewer": reviewer, "keeper": k[i18n.SI]} {
			if p := b.get(path); p.status != 200 {
				t.Errorf("%s, %s: %d", name, path, p.status)
			}
		}
	}
	if p := reviewer.get("/glossary/waiting"); p.status != 403 {
		t.Errorf("a reviewer has no waiting work: %d", p.status)
	}
	if p := admin.get("/glossary/waiting"); p.status != 200 {
		t.Errorf("%d", p.status)
	}
	second := k[i18n.EN].propose(glossary.TypeTerm, "Province", "පළාත", "மாகாணம்", "")
	if p := admin.check(second, i18n.EN, ""); p.status != 403 {
		t.Errorf("an administrator is not a keeper: %d", p.status)
	}
	// Signed out, nothing opens; a form without its token changes nothing.
	if p := w.browser().get("/glossary"); !strings.Contains(p.body, "signin") && p.url != "/signin" {
		t.Errorf("%d %s", p.status, p.url)
	}
	if p := k[i18n.EN].post(fmt.Sprintf("/glossary/%d/check", second), url.Values{"language": {"en"}}); p.status != 403 {
		t.Errorf("no token: %d", p.status)
	}
	if e := w.entry(second); len(e.Unchecked()) != 3 {
		t.Fatalf("%+v", e)
	}
}

// R-GLO-02: never deleted, only replaced; the replaced entry keeps its history. R-GLO-05: any
// past version can be read. A language that reads as it did keeps its check.
func TestACorrectionReplacesAndHistoryStays(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypePost, "Chief Clerk", "ප්‍රධාන ලිපිකරු", "பிரதம எழுத்தர்", "")
	w.publish(id, k)
	v1 := w.version()
	correct := url.Values{"csrf": {k[i18n.TA].csrf()}, "replaces": {itoa(id)}, "type": {glossary.TypePost}, "text_en": {"Chief Clerk"}, "text_si": {"ප්‍රධාන ලිපිකරු"}, "text_ta": {"தலைமை எழுத்தர்"}, "note": {""}}
	p := k[i18n.TA].post("/glossary/propose", correct)
	m := entryPath.FindStringSubmatch(p.url)
	if m == nil {
		t.Fatalf("%d %s %s", p.status, p.url, p.body)
	}
	var next int64
	fmt.Sscan(m[1], &next)
	e := w.entry(next)
	if e.Status != glossary.StatusHeld || len(e.Unchecked()) != 1 || e.Unchecked()[0] != i18n.TA {
		t.Fatalf("only Tamil is concerned: %+v", e)
	}
	// A second correction waits for the first; the first is still in force meanwhile.
	correct.Set("csrf", k[i18n.TA].csrf())
	says(t, k[i18n.TA].post("/glossary/propose", correct), i18n.TA, "glossaryCorrectionWaits")
	if old := w.entry(id); old.Status != glossary.StatusPublished {
		t.Fatal(old.Status)
	}
	k[i18n.TA].check(next, i18n.TA, "")
	old, now := w.entry(id), w.entry(next)
	if old.Status != glossary.StatusReplaced || old.ReplacedIn == nil || *old.ReplacedIn != v1+1 || now.Status != glossary.StatusPublished || *now.PublishedIn != v1+1 {
		t.Fatalf("%+v %+v", old, now)
	}
	if old.Term(i18n.TA).Text != "பிரதம எழுத்தர்" {
		t.Fatal("the replaced entry lost its words")
	}
	ctx := context.Background()
	then, err := glossary.AtVersion(ctx, w.pool, v1, "")
	if err != nil || len(then) != 1 || then[0].ID != id {
		t.Fatalf("%+v %v", then, err)
	}
	later, err := glossary.AtVersion(ctx, w.pool, v1+1, "")
	if err != nil || len(later) != 1 || later[0].ID != next {
		t.Fatalf("%+v %v", later, err)
	}
	b := k[i18n.SI]
	if p := b.get(fmt.Sprintf("/glossary-versions/%d", v1)); !strings.Contains(p.body, "பிரதம எழுத்தர்") || strings.Contains(p.body, "தலைமை எழுத்தர்") {
		t.Fatal(p.body)
	}
	if p := b.get(fmt.Sprintf("/glossary/%d/history", next)); !strings.Contains(p.body, "பிரதம எழுத்தர்") || !strings.Contains(p.body, "தலைமை எழுத்தர்") {
		t.Fatal(p.body)
	}
	if p := b.get("/glossary"); strings.Contains(p.body, "பிரதம எழுத்தர்") {
		t.Fatal("a replaced term still listed as in force")
	}
	if p := b.get("/glossary-versions/999999"); p.status != 404 {
		t.Fatal(p.status)
	}
}

// A note or type alone changing is for all three keepers, and a correction of nothing is refused.
func TestACorrectionOfNothingIsRefused(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypePost, "Registrar", "රෙජිස්ට්‍රාර්", "பதிவாளர்", "")
	w.publish(id, k)
	f := url.Values{"csrf": {k[i18n.EN].csrf()}, "replaces": {itoa(id)}, "type": {glossary.TypePost}, "text_en": {"Registrar"}, "text_si": {"රෙජිස්ට්‍රාර්"}, "text_ta": {"பதிவாளர்"}, "note": {""}}
	says(t, k[i18n.EN].post("/glossary/propose", f), i18n.EN, "glossaryNothingChanged")
	f.Set("note", "An invented note.")
	p := k[i18n.EN].post("/glossary/propose", f)
	m := entryPath.FindStringSubmatch(p.url)
	if m == nil {
		t.Fatalf("%d %s", p.status, p.url)
	}
	var next int64
	fmt.Sscan(m[1], &next)
	if e := w.entry(next); e.Status != glossary.StatusProposed || len(e.Unchecked()) != 3 {
		t.Fatalf("%+v", e)
	}
}

// What the form refuses, and that a text comes out one way: a term another published entry holds
// is refused when proposed, when corrected, and when a rival gets there first.
func TestRefusalsOnTheForm(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	first := k[i18n.EN].propose(glossary.TypePost, "Auditor", "විගණක", "கணக்காய்வாளர்", "")
	rival := k[i18n.EN].propose(glossary.TypeTerm, "The Auditor", "විගණක", "தணிக்கையாளர்", "")
	w.publish(first, k)
	k2 := k[i18n.SI]
	says(t, k2.check(rival, i18n.SI, ""), i18n.SI, "glossaryDuplicate")
	b := k[i18n.EN]
	form := func(kind, en, si, ta string) url.Values {
		return url.Values{"csrf": {b.csrf()}, "type": {kind}, "text_en": {en}, "text_si": {si}, "text_ta": {ta}}
	}
	for word, f := range map[string]url.Values{
		"glossaryDuplicate":   form(glossary.TypeTerm, "auditor", "", ""),
		"glossaryBadType":     form("nonsense", "Word", "", ""),
		"glossaryNoTerms":     form(glossary.TypeTerm, "", "", ""),
		"glossaryBadText":     form(glossary.TypeTerm, "Call 0771234567", "", ""),
		"glossaryKeepDiffers": form(glossary.TypeKeep, "SCOS Pro", "", "வேறு"),
	} {
		says(t, b.post("/glossary/propose", f), i18n.EN, word)
	}
	if p := b.post("/glossary/999999/check", url.Values{"csrf": {b.csrf()}, "language": {"en"}}); p.status != 404 {
		t.Error(p.status)
	}
	if p := b.post("/glossary/nonsense/check", url.Values{"csrf": {b.csrf()}, "language": {"en"}}); p.status != 404 {
		t.Error(p.status)
	}
}

// A refusal stays with the entry and says why; a language already checked has no say.
func TestAKeeperRefusesAnEntry(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypeTerm, "Gazette", "ගැසට්", "வர்த்தமானி", "")
	k[i18n.EN].check(id, i18n.EN, "")
	refuse := func(b *browser, l i18n.Lang, note string) page {
		return b.post(fmt.Sprintf("/glossary/%d/refuse", id), url.Values{"csrf": {b.csrf()}, "language": {string(l)}, "note": {note}})
	}
	says(t, refuse(k[i18n.EN], i18n.EN, "no"), i18n.EN, "glossaryAlreadyChecked")
	says(t, refuse(k[i18n.TA], i18n.TA, "  "), i18n.TA, "glossaryNoOutcomeNote")
	says(t, refuse(k[i18n.TA], i18n.TA, "An invented reason."), i18n.TA, "glossaryRefused")
	if e := w.entry(id); e.Status != glossary.StatusRefused || e.OutcomeNote != "An invented reason." {
		t.Fatalf("%+v", e)
	}
	says(t, k[i18n.SI].check(id, i18n.SI, ""), i18n.SI, "glossaryNotWaiting")
}

// The words are stored one way however they were typed, and found again the same way (R-NFR-05).
func TestWordsAreNormalisedOnTheScreens(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	k := w.keepers()
	decomposed := "Café Office"
	id := k[i18n.EN].propose(glossary.TypeInstitution, decomposed, "ගැසට්", "வர்த்தமானி", "")
	if got := w.entry(id).Term(i18n.EN).Text; got != "Café Office" {
		t.Fatalf("%q", got)
	}
	w.publish(id, k)
	if p := k[i18n.SI].get("/glossary?find=" + url.QueryEscape(decomposed)); !strings.Contains(p.body, "Café Office") {
		t.Fatal(p.body)
	}
	if p := k[i18n.SI].get("/glossary?find=" + url.QueryEscape("cafÉ")); !strings.Contains(p.body, "Café Office") {
		t.Fatal(p.body)
	}
	if p := k[i18n.SI].get("/glossary?find=" + url.QueryEscape("%")); strings.Contains(p.body, "Café Office") {
		t.Fatal("a % found everything")
	}
}

// R-REC-02 and the definition of done: each change is an outbox entry with `not known` as post,
// and no word of the glossary reaches a log or the outbox, even when it is refused.
func TestNoGlossaryWordReachesALogOrTheOutbox(t *testing.T) {
	w := newWorld(t)
	w.fresh()
	w.glossaryClean()
	const secret = "Inventedmarkerword"
	k := w.keepers()
	id := k[i18n.EN].propose(glossary.TypeTerm, secret+" one", "ගැසට් "+secret, "வர்த்தமானி "+secret, secret+" note")
	k[i18n.EN].check(id, i18n.EN, secret+" two")
	k[i18n.SI].check(id, i18n.SI, "")
	k[i18n.TA].check(id, i18n.TA, "")
	refused := k[i18n.EN].propose(glossary.TypeTerm, secret+" three", "", "", "")
	k[i18n.TA].post(fmt.Sprintf("/glossary/%d/refuse", refused), url.Values{"csrf": {k[i18n.TA].csrf()}, "language": {"ta"}, "note": {secret + " why"}})
	b := k[i18n.EN]
	b.post("/glossary/propose", url.Values{"csrf": {b.csrf()}, "type": {"term"}, "text_en": {secret + " 0771234567"}})
	b.get("/glossary?find=" + secret)

	entries := w.outbox()
	got := map[string]int{}
	for _, x := range entries {
		got[x.Action+" "+x.ObjectType]++
		if strings.Contains(x.Raw, secret) {
			t.Errorf("in the outbox: %s", x.Raw)
		}
		if x.Post != "not known" {
			t.Errorf("post %q", x.Post)
		}
	}
	for want, n := range map[string]int{"creation glossary entry": 2, "approval glossary entry": 3, "change glossary version": 1, "rejection glossary entry": 1} {
		if got[want] != n {
			t.Errorf("%s: %d, want %d", want, got[want], n)
		}
	}
	if strings.Contains(w.logs.String(), secret) {
		t.Errorf("in the logs: %s", w.logs.String())
	}
}

// Every screen in each language, with nothing typed reaching a message.
func TestGlossaryScreensInEachLanguage(t *testing.T) {
	e := glossary.Entry{ID: 7, Type: glossary.TypePost, Status: glossary.StatusHeld, Note: "An invented note.", Terms: []glossary.Term{
		{Language: i18n.SI, Text: "ප්‍රධාන ලේකම්"}, {Language: i18n.TA}, {Language: i18n.EN, Text: "Chief Secretary"}}}
	for _, l := range i18n.Langs {
		for _, c := range []struct {
			page string
			data any
			want []string
		}{
			{"glossary", glossaryListData{Entries: []glossary.Entry{e}, Version: 3, MayKeep: true}, []string{"Chief Secretary", `href="/glossary/7"`, i18n.T(l, "glossaryPropose"), i18n.T(l, "glossaryNotWritten")}},
			{"glossary-waiting", glossaryWaitingData{Entries: []glossary.Entry{e}, Yours: []i18n.Lang{l}}, []string{i18n.T(l, "glossaryWaitingYours")}},
			{"glossary-entry", glossaryEntryData{Entry: e, Yours: []glossary.Term{e.Term(i18n.TA)}}, []string{"band-si", "band-ta", "band-en", `action="/glossary/7/check"`, `action="/glossary/7/refuse"`, `name="language" value="ta"`, `lang="si"`}},
			{"glossary-propose", glossaryFormData{Types: glossary.Types}, []string{`name="text_si"`, `name="text_ta"`, `name="text_en"`, i18n.T(l, "typeKeep")}},
			{"glossary-propose", glossaryFormData{Types: glossary.Types, Entry: &e}, []string{`name="replaces" value="7"`, `value="Chief Secretary"`}},
			{"glossary-versions", glossaryVersionsData{Versions: []glossary.Version{{Number: 3, Added: 1}}}, []string{`href="/glossary-versions/3"`}},
			{"glossary-version", glossaryVersionData{Version: glossary.Version{Number: 3}, Entries: []glossary.Entry{e}}, []string{"Chief Secretary", i18n.T(l, "glossaryVersionNote")}},
			{"glossary-history", glossaryHistoryData{ID: 7, Entries: []glossary.Entry{e}}, []string{"Chief Secretary"}},
		} {
			body := render(t, c.page, Page{Lang: l, Path: "/glossary", Title: "glossary", Here: "glossary", CSRF: "form-token", Viewer: &Viewer{Name: "Test"}, Data: c.data})
			for _, want := range c.want {
				if !strings.Contains(body, want) {
					t.Errorf("%s %s: no %q", c.page, l, want)
				}
			}
		}
	}
}
