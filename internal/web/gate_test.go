package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

func TestGateScreenInEachLanguage(t *testing.T) {
	list := twoSources()
	on := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	policy := sources.Policy{Gates: map[sources.GateKey]sources.Gate{
		{Source: "src-a", From: "en", To: "ta"}: {State: sources.GateOpen, SourceVersion: "1", TestSet: "shared-1", Score: 81.5, EvaluatedOn: on},
		{Source: "src-b", From: "en", To: "ta"}: {State: sources.GateOpen, SourceVersion: "0", TestSet: "shared-1", Score: 70, EvaluatedOn: on},
	}}
	score := 81.5
	for _, l := range i18n.Langs {
		body := render(t, "gate", Page{Lang: l, Path: "/gate", Title: "gate", Here: "gate", CSRF: "form-token", Viewer: &Viewer{Name: "Test"},
			Data: gateData{Rows: gateRows(list, policy), Today: "2030-06-01", History: []sources.GateRow{{Source: "src-a", From: "en", To: "ta", SourceVersion: "1", State: "open", TestSet: "shared-1", Score: &score, EvaluatedOn: &on, Since: on, By: "Test Administrator"}}}})
		for _, want := range []string{
			`action="/gate/src-a/en/ta/close"`, `action="/gate/src-b/en/ta/open"`, "shared-1", "81.5", i18n.T(l, "gateNewVersion"),
			`for="set-src-b-en-ta"`, `name="csrf" value="form-token"`, `max="2030-06-01"`, "Test Administrator",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
	}
}

// A new version starts closed: the screen and the rule agree (R-GAT-01).
func TestGateRowsFollowTheVersion(t *testing.T) {
	list := twoSources()
	key := sources.GateKey{Source: "src-a", From: "en", To: "ta"}
	p := sources.Policy{Gates: map[sources.GateKey]sources.Gate{key: {State: sources.GateOpen, SourceVersion: "1"}}}
	rows := gateRows(list, p)
	if len(rows) != 2 || !rows[0].Open || rows[0].NewVersion || rows[1].Open {
		t.Fatalf("%+v", rows)
	}
	list.Sources[0].Version = "2"
	rows = gateRows(list, p)
	if rows[0].Open || !rows[0].NewVersion {
		t.Fatalf("%+v", rows[0])
	}
}

func TestGateOpensOnAnEvaluationAndKeepsHistory(t *testing.T) {
	const marker = "CALLERTEXTMARKER"
	w := newWorld(t)
	list := twoSources()
	w.app.deps.Sources = fixedList{sources.Snapshot{List: list, Fresh: true}}
	admin, who := w.signedInAdmin()
	w.freshSources(list)
	t.Cleanup(func() { w.freshSources(bumblebee.List{}) })
	w.exec(`DELETE FROM audit_outbox`)
	token := admin.csrf()
	ctx := context.Background()
	d := sources.Translate("en", "ta")
	src := list.Sources[0]
	open := func() bool {
		p, err := sources.LoadPolicy(ctx, w.pool)
		if err != nil {
			t.Fatal(err)
		}
		return p.Open(src, d.From, d.To)
	}
	if open() {
		t.Fatal("closed until opened")
	}
	// Each part of the evaluation is required, and what was typed is not repeated.
	for _, form := range []url.Values{
		{"csrf": {token}},
		{"csrf": {token}, "test_set": {"shared-1"}, "score": {marker}, "evaluated_on": {"2030-01-01"}, "reason": {"Invented."}},
		{"csrf": {token}, "test_set": {"shared-1"}, "score": {"80"}, "reason": {"Invented."}},
		{"csrf": {token}, "test_set": {"shared-1"}, "score": {"80"}, "evaluated_on": {"2020-01-01"}},
		{"csrf": {token}, "test_set": {"shared-1"}, "score": {"80"}, "evaluated_on": {"2020-01-01"}, "reason": {"   "}},
		{"csrf": {token}, "test_set": {"shared-1"}, "score": {"80"}, "evaluated_on": {"2020-01-01"}, "reason": {"Card 123456789012 and invented."}},
		{"csrf": {token}, "test_set": {"shared-1"}, "score": {"80"}, "evaluated_on": {"2999-01-01"}, "reason": {"Invented."}},
		{"csrf": {token}, "test_set": {"123456789012"}, "score": {"80"}, "evaluated_on": {"2030-01-01"}, "reason": {"Invented."}},
	} {
		if p := admin.post("/gate/src-a/en/ta/open", form); !strings.Contains(p.body, esc(i18n.T(i18n.EN, "gateBad"))) || strings.Contains(p.body, marker) {
			t.Fatalf("%v: %d", form, p.status)
		}
	}
	if open() {
		t.Fatal("opened without an evaluation")
	}
	good := url.Values{"csrf": {token}, "test_set": {"shared-1"}, "score": {"81.5"}, "evaluated_on": {"2020-01-01"}, "reason": {"An invented basic run."}}
	if p := admin.post("/gate/src-a/en/ta/open", good); !strings.Contains(p.body, esc(i18n.T(i18n.EN, "gateOpened"))) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if !open() {
		t.Fatal("not open")
	}
	got := w.outbox()
	if len(got) != 1 || got[0].Action != "change" || got[0].ObjectType != "gate" || got[0].ObjectRef != "src-a translate en-ta" || got[0].Actor != who || got[0].Details["score"] != "81.5" || got[0].Details["source_version"] != "1" {
		t.Fatalf("%+v", got)
	}
	// A new version in the list starts closed, whatever the table says.
	src.Version = "2"
	if open() {
		t.Fatal("a new version stays open")
	}
	src.Version = "1"
	if p := admin.post("/gate/src-a/en/ta/close", url.Values{"csrf": {token}}); !strings.Contains(p.body, esc(i18n.T(i18n.EN, "gateClosed"))) {
		t.Fatal(p.body)
	}
	if open() {
		t.Fatal("not closed")
	}
	// Both decisions stay as history, and one is in force.
	var all, current int
	w.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE effective_to IS NULL) FROM gate_decision`).Scan(&all, &current)
	if all != 2 || current != 1 {
		t.Fatal(all, current)
	}
	if p := admin.get("/gate"); p.status != 200 || !strings.Contains(p.body, "shared-1") || !strings.Contains(p.body, "Test "+who) ||
		!strings.Contains(p.body, "An invented basic run.") || !strings.Contains(p.body, esc(i18n.T(i18n.EN, "gateUnchecked"))) {
		t.Fatalf("%d %s", p.status, p.body)
	}
	if p := admin.post("/gate/nothing/en/ta/close", url.Values{"csrf": {token}}); !strings.Contains(p.body, "not on the list") {
		t.Fatal(p.body)
	}
	if p := admin.post("/gate/src-a/en/en/close", url.Values{"csrf": {token}}); p.status != 404 {
		t.Fatal(p.status)
	}
	if strings.Contains(w.logs.String(), marker) {
		t.Fatal("a log holds what was typed")
	}
	// The administrator's alone, and a try by anyone else is a denial.
	owner := w.browser()
	owner.signIn(subject("officer"))
	if p := owner.get("/gate"); p.status != 403 {
		t.Fatal(p.status)
	}
	if p := owner.post("/gate/src-a/en/ta/open", good); p.status != 403 {
		t.Fatal(p.status)
	}
}

// A closed decision has no test set, score, date or end yet: at phone width a label with
// nothing after it would stand alone.
func TestGateHistoryShowsNoEmptyLabels(t *testing.T) {
	out := render(t, "gate", Page{Lang: i18n.EN, Path: "/gate", Title: "gate", Viewer: &Viewer{Name: "Test"},
		Data: gateData{Today: "2030-06-01", History: []sources.GateRow{{Source: "src-a", From: "en", To: "ta", SourceVersion: "1", State: "closed", By: "Test Administrator"}}}})
	for _, word := range []string{"gateTestSet", "gateScore", "gateEvaluatedOn", "until"} {
		if strings.Contains(out, `<span class="stack-label">`+i18n.T(i18n.EN, word)+`: </span>`) {
			t.Errorf("%s stands alone", word)
		}
	}
}
