package web

import (
	"context"
	"html/template"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

func someList() bumblebee.List {
	rest := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	return bumblebee.List{MaxTextChars: 4321, Sources: []bumblebee.Source{
		{Name: "standin-translate", Version: "1", State: "on", Offers: []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}},
		{Name: "standin-resting", Version: "3", State: "resting-block", RestUntil: &rest},
	}}
}

// R-NFR-13: every source is said to be an outside website, beside it.
func TestSourcesScreenSaysEachIsAnOutsideWebsite(t *testing.T) {
	for _, l := range i18n.Langs {
		body := render(t, "sources", Page{Lang: l, Path: "/sources", Title: "sources", Viewer: &Viewer{Name: "Test"},
			Data: buildSourcesData(sources.Snapshot{List: someList(), ReadAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), Fresh: true}, 4321, sources.Policy{}, false)})
		for _, want := range []string{"4321", "standin-resting", i18n.T(l, "stateRestBlock"), "2030-01-01 10:00", esc(i18n.T(l, "sourcesNote"))} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		if n := strings.Count(body, i18n.T(l, "outsideWebsite")); n != 2 {
			t.Errorf("%s: said %d times", l, n)
		}
		if strings.Contains(body, esc(i18n.T(l, "listNotRead"))) {
			t.Errorf("%s: a fresh list is called stale", l)
		}
	}
}

func TestSourcesScreenSaysWhenTheListCannotBeRead(t *testing.T) {
	body := render(t, "sources", Page{Lang: i18n.EN, Path: "/sources", Title: "sources", Viewer: &Viewer{Name: "Test"}, Data: buildSourcesData(sources.Snapshot{}, sources.DefaultMaxText, sources.Policy{}, false)})
	if !strings.Contains(body, esc(i18n.T(i18n.EN, "listNotRead"))) || !strings.Contains(body, i18n.T(i18n.EN, "noSources")) {
		t.Fatal(body)
	}
}

// The administrator has the forms; a system owner sees the same list and order, with
// nothing to change.
func TestSourcesScreenFormsAreTheAdministrators(t *testing.T) {
	d := sources.Translate("en", "ta")
	list := someList()
	list.Sources = append(list.Sources, bumblebee.Source{Name: "standin-b", Version: "1", State: "on", Offers: []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}})
	policy := sources.Policy{Orders: map[sources.Direction][]string{d: {"standin-b"}}, Off: map[string]bool{"standin-resting": true}}
	snap := sources.Snapshot{List: list, Fresh: true}
	for _, l := range i18n.Langs {
		admin := render(t, "sources", Page{Lang: l, Path: "/sources", Title: "sources", CSRF: "form-token", Viewer: &Viewer{Name: "Test"}, Data: buildSourcesData(snap, 10, policy, true)})
		for _, want := range []string{`action="/sources/order/translate/en/ta"`, `name="pos:standin-b"`, `value="1"`, `action="/sources/switch/standin-translate"`, `name="on" value="false"`, `action="/sources/switch/standin-resting"`, `name="on" value="true"`, `href="/sources/history"`, i18n.Name(i18n.EN) + " \u2192 " + i18n.Name(i18n.TA)} {
			if !strings.Contains(admin, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		owner := render(t, "sources", Page{Lang: l, Path: "/sources", Title: "sources", Viewer: &Viewer{Name: "Test"}, Data: buildSourcesData(snap, 10, policy, false)})
		if strings.Contains(owner, `action="/sources/switch`) || !strings.Contains(owner, `disabled`) || strings.Contains(owner, `/sources/history`) {
			t.Errorf("%s: an owner can change the order", l)
		}
		hist := render(t, "sources-history", Page{Lang: l, Path: "/sources/history", Title: "sourcesHistory", Viewer: &Viewer{Name: "Test"},
			Data: []sources.HistoryRow{{Subject: "source order", Ref: "translate en-ta", Old: "a", New: "b, a", By: "Test Administrator"}}})
		if !strings.Contains(hist, "translate en-ta") || !strings.Contains(hist, "Test Administrator") {
			t.Errorf("%s: %s", l, hist)
		}
	}
}

// A source off keeps its place in the form, so turning it on again changes nothing.
func TestOrderFormKeepsASourceTurnedOff(t *testing.T) {
	d := sources.Translate("en", "ta")
	list := bumblebee.List{Sources: []bumblebee.Source{
		{Name: "a", State: "on", Offers: []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}},
		{Name: "b", State: "on", Offers: []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}},
	}}
	got := buildSourcesData(sources.Snapshot{List: list}, 10, sources.Policy{Orders: map[sources.Direction][]string{d: {"b"}}, Off: map[string]bool{"b": true}}, true)
	if len(got.Orders) != 1 || got.Orders[0].Rows[0].Name != "b" || got.Orders[0].Rows[0].Position != 1 || got.Orders[0].Rows[1].Position != 0 {
		t.Fatalf("%+v", got.Orders)
	}
	if got.Rows[1].OnForRosetta {
		t.Fatal("b is off")
	}
}

func TestDirectionNameSaysBothLanguages(t *testing.T) {
	if got := directionName(i18n.TA, sources.Grammar); got != i18n.T(i18n.TA, "grammarEnglish") {
		t.Fatal(got)
	}
	if got := directionName(i18n.EN, sources.Translate("si", "ta")); !strings.Contains(got, i18n.Name(i18n.SI)) || !strings.Contains(got, i18n.Name(i18n.TA)) {
		t.Fatal(got)
	}
}

func TestStateWords(t *testing.T) {
	for in, want := range map[string]string{"on": "stateOn", "resting-block": "stateRestBlock", "resting-change": "stateRestChange", "off": "stateOff", "later": "stateOff"} {
		if stateWord(in) != want {
			t.Errorf("%q", in)
		}
	}
}

func esc(s string) string { return template.HTMLEscapeString(s) }

type fixedList struct{ snap sources.Snapshot }

func (f fixedList) Snapshot(context.Context) sources.Snapshot { return f.snap }
func (f fixedList) MaxText() int                              { return f.snap.List.MaxTextChars }

func TestSourcesScreenOpensForAdministratorAndOwnerOnly(t *testing.T) {
	w := newWorld(t)
	w.app.deps.Sources = fixedList{sources.Snapshot{List: someList(), Fresh: true}}
	admin, _ := w.signedInAdmin()
	if p := admin.get("/sources"); p.status != 200 || !strings.Contains(p.body, "standin-translate") {
		t.Fatalf("%d", p.status)
	}
	nobody := w.browser()
	nobody.signIn(subject("officer"))
	if p := nobody.get("/sources"); p.status != 403 {
		t.Fatalf("%d", p.status)
	}
}

// ---- with the database: an order and a switch, their history and their entries ---

func (w *world) freshSources(list bumblebee.List) {
	w.t.Helper()
	w.exec(`DELETE FROM cache_piece`)
	w.exec(`DELETE FROM source_order`)
	w.exec(`DELETE FROM gate_decision`)
	w.exec(`DELETE FROM source_offer`)
	w.exec(`DELETE FROM source`)
	w.exec(`DELETE FROM change_history`)
	if err := sources.Mirror(context.Background(), w.pool, list, time.Now()); err != nil {
		w.t.Fatal(err)
	}
}

func twoSources() bumblebee.List {
	offer := []bumblebee.Offer{{Operation: "translate", From: "en", To: "ta"}}
	return bumblebee.List{MaxTextChars: 100, Sources: []bumblebee.Source{{Name: "src-a", Version: "1", State: "on", Offers: offer}, {Name: "src-b", Version: "1", State: "on", Offers: offer}}}
}

func TestOrderAndSwitchKeepHistoryAndEntries(t *testing.T) {
	w := newWorld(t)
	list := twoSources()
	w.app.deps.Sources = fixedList{sources.Snapshot{List: list, Fresh: true}}
	admin, who := w.signedInAdmin()
	w.freshSources(list)
	t.Cleanup(func() { w.freshSources(bumblebee.List{}) })
	w.exec(`DELETE FROM audit_outbox`)
	token := admin.csrf()
	ctx := context.Background()
	p := admin.post("/sources/order/translate/en/ta", url.Values{"csrf": {token}, "pos:src-b": {"1"}, "pos:src-a": {"2"}})
	if p.status != 200 || !strings.Contains(p.body, "Saved") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	policy, err := sources.LoadPolicy(ctx, w.pool)
	if err != nil {
		t.Fatal(err)
	}
	if got := policy.Orders[sources.Translate("en", "ta")]; len(got) != 2 || got[0] != "src-b" {
		t.Fatal(got)
	}
	// The same order again changes nothing; a bad one is refused and not repeated.
	admin.post("/sources/order/translate/en/ta", url.Values{"csrf": {token}, "pos:src-b": {"1"}, "pos:src-a": {"2"}})
	if p := admin.post("/sources/order/translate/en/ta", url.Values{"csrf": {token}, "pos:src-a": {"CALLERTEXTMARKER"}}); !strings.Contains(p.body, "whole numbers") || strings.Contains(p.body, "CALLERTEXTMARKER") {
		t.Fatal(p.body)
	}
	if p := admin.post("/sources/order/translate/en/en", url.Values{"csrf": {token}}); p.status != 404 {
		t.Fatalf("%d", p.status)
	}
	if p := admin.post("/sources/switch/src-a", url.Values{"csrf": {token}, "on": {"false"}}); !strings.Contains(p.body, "Saved") {
		t.Fatal(p.body)
	}
	if p := admin.post("/sources/switch/nothing", url.Values{"csrf": {token}, "on": {"false"}}); !strings.Contains(p.body, "not on the list") {
		t.Fatal(p.body)
	}
	policy, _ = sources.LoadPolicy(ctx, w.pool)
	if !policy.Off["src-a"] {
		t.Fatal("not off")
	}
	got := w.outbox()
	if len(got) != 2 || got[0].Action != "change" || got[0].ObjectType != "source order" || got[0].ObjectRef != "translate en-ta" || got[0].Actor != who || got[1].ObjectType != "source" || got[1].ObjectRef != "src-a" {
		t.Fatalf("%+v", got)
	}
	if h := admin.get("/sources/history"); h.status != 200 || !strings.Contains(h.body, "src-b, src-a") || !strings.Contains(h.body, "translate en-ta") {
		t.Fatalf("%d %s", h.status, h.body)
	}
	var n int
	w.pool.QueryRow(ctx, `SELECT count(*) FROM change_history`).Scan(&n)
	if n != 2 {
		t.Fatal(n)
	}
	// Nobody else changes anything, and each try is a denial.
	nobody := w.browser()
	nobody.signIn(subject("officer"))
	if p := nobody.post("/sources/switch/src-a", url.Values{"csrf": {nobody.csrf()}, "on": {"true"}}); p.status != 403 {
		t.Fatalf("%d", p.status)
	}
}
