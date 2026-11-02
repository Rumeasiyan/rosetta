package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/alerts"
	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

// R-ADM-02 and R-NFR-13: BUMBLEBEE up or down, each source's state and resume time beside the words
// saying it is an outside site, and today's requests, refusals and failures by reason.
func TestStatusShowsWhatRAdm02Asks(t *testing.T) {
	w := newWorld(t)
	admin, owner, _, _ := w.usageWorld()
	rest := time.Now().Add(time.Hour)
	w.app.deps.Sources = fixedList{sources.Snapshot{Fresh: true, ReadAt: time.Now(), List: bumblebee.List{Sources: []bumblebee.Source{
		{Name: "site-on", Version: "1", State: bumblebee.StateOn}, {Name: "site-rest", Version: "1", State: bumblebee.StateRestingBlock, RestUntil: &rest},
	}}}}
	w.app.deps.NoSystem = func() map[reasons.Reason]int { return map[reasons.Reason]int{reasons.NoKey: 4} }
	p := admin.get("/status")
	if p.status != 200 {
		t.Fatalf("%d %s", p.status, p.body)
	}
	for _, want := range []string{"BUMBLEBEE is answering", "site-rest", "Resting after a block", When(rest), "An outside public website", "Requests today: <strong>3", "Refusals today: <strong>0", "Failures today: <strong>1", "No provider", "No key: <strong>4", "Waiting to reach PENSIEVE"} {
		if !strings.Contains(p.body, want) {
			t.Errorf("no %q", want)
		}
	}
	// The latest alerts show, and whether each has gone.
	if err := alerts.Raise(context.Background(), w.pool, alerts.Alert{Kind: alerts.BumblebeeDown}); err != nil {
		t.Fatal(err)
	}
	if p := admin.get("/status"); !strings.Contains(p.body, "BUMBLEBEE stopped answering") || !strings.Contains(p.body, "Waiting to go") {
		t.Fatal(p.body)
	}
	// What an alert is about reads in the reader's language, where the words are ROSETTA's.
	if got := alertAbout(i18n.TA, alerts.Recent{Kind: alerts.CallerFlagged, Subject: callers.FlagIdle}); got != i18n.T(i18n.TA, "flagIdle") {
		t.Fatal(got)
	}
	if got := alertAbout(i18n.SI, alerts.Recent{Kind: alerts.LanguageFlagged, Subject: "ta"}); got != i18n.Name(i18n.TA) {
		t.Fatal(got)
	}
	if got := alertAbout(i18n.EN, alerts.Recent{Kind: alerts.NoSource, Subject: sources.Translate("en", "ta").String()}); !strings.Contains(got, "English") {
		t.Fatal(got)
	}
	// An owner sees the sources and their own counts, no other system's and nothing unattributed.
	p = owner.get("/status")
	if p.status != 200 || !strings.Contains(p.body, "site-rest") || !strings.Contains(p.body, "Requests today: <strong>2") || strings.Contains(p.body, "No key:") || strings.Contains(p.body, "PENSIEVE") || strings.Contains(p.body, "BUMBLEBEE stopped") {
		t.Fatalf("%d %s", p.status, p.body)
	}
	w.app.deps.Sources = fixedList{sources.Snapshot{}}
	if p := admin.get("/status"); !strings.Contains(p.body, "BUMBLEBEE is not answering") {
		t.Fatal(p.body)
	}
	for _, lang := range []string{"si", "ta"} {
		admin.get("/lang/" + lang)
		if p := admin.get("/status"); p.status != 200 || strings.Contains(p.body, "Requests today") {
			t.Errorf("%s: %d", lang, p.status)
		}
	}
	admin.get("/lang/en")
	nobody := w.browser()
	nobody.signIn(subject("officer"))
	if p := nobody.get("/status"); p.status != 403 {
		t.Fatalf("no role: %d", p.status)
	}
}
