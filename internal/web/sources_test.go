package web

import (
	"context"
	"html/template"
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
			Data: sourcesData{Snapshot: sources.Snapshot{List: someList(), ReadAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), Fresh: true}, MaxText: 4321}})
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
	body := render(t, "sources", Page{Lang: i18n.EN, Path: "/sources", Title: "sources", Viewer: &Viewer{Name: "Test"}, Data: sourcesData{MaxText: sources.DefaultMaxText}})
	if !strings.Contains(body, esc(i18n.T(i18n.EN, "listNotRead"))) || !strings.Contains(body, i18n.T(i18n.EN, "noSources")) {
		t.Fatal(body)
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

type fixedList struct{ d sourcesData }

func (f fixedList) Snapshot(context.Context) sources.Snapshot { return f.d.Snapshot }
func (f fixedList) MaxText() int                              { return f.d.MaxText }

func TestSourcesScreenOpensForAdministratorAndOwnerOnly(t *testing.T) {
	w := newWorld(t)
	w.app.deps.Sources = fixedList{sourcesData{Snapshot: sources.Snapshot{List: someList(), Fresh: true}, MaxText: 4321}}
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
