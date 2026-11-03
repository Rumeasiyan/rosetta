package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
)

func TestActivityPartsAreThoseDeclared(t *testing.T) {
	raw, err := os.ReadFile("../../docs/cerberus-declaration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	for _, m := range regexp.MustCompile(`(?m)^  - path: (\S+)\n    level: highly_sensitive$`).FindAllStringSubmatch(string(raw), -1) {
		declared = append(declared, m[1])
	}
	if !slices.Equal(declared, activityParts) {
		t.Fatalf("declared %v, signal covers %v", declared, activityParts)
	}
}

func TestActivityGoesUnderThePagesOwnPart(t *testing.T) {
	for path, want := range map[string]string{
		"/":                "/activity",
		"/glossary":        "/activity",
		"/glossary/12":     "/activity",
		"/glossary/import": "/glossary/import/activity",
		"/memory":          "/memory/activity",
		"/memory/7":        "/memory/activity",
		"/systems/3/test":  "/systems/activity",
		"/statusquo":       "/activity",
		"":                 "/activity",
	} {
		if got := activityPath(path); got != want {
			t.Errorf("%q: %q, want %q", path, got, want)
		}
	}
}

func TestEverySignedInPageCarriesTheSignal(t *testing.T) {
	for _, l := range i18n.Langs {
		body := render(t, "message", Page{Lang: l, Path: "/memory/7", Title: "rosetta", Viewer: &Viewer{Name: "Test Reviewer"}})
		for _, want := range []string{`src="` + StaticPrefix + `activity.js"`, `id="activity"`, `data-url="/memory/activity"`, `hidden`, i18n.T(l, "activityLost")} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
	}
	if body := render(t, "message", Page{Lang: i18n.EN, Path: "/", Title: "rosetta"}); strings.Contains(body, `id="activity"`) {
		t.Error("a page with nobody signed in carries the note")
	}
}

func TestActivityAnswersWithNoWork(t *testing.T) {
	mux := http.NewServeMux()
	(&App{}).mountActivity(mux)
	for _, path := range append([]string{activitySuffix}, activityParts...) {
		target := path
		if path != activitySuffix {
			target += activitySuffix
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, target, nil))
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Errorf("%s: %d %q", target, rec.Code, rec.Body)
		}
	}
}

func TestTheSignalIsNeverOnATimer(t *testing.T) {
	raw, err := os.ReadFile("static/activity.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"setInterval", "setTimeout", "requestAnimationFrame"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("activity.js uses %s", banned)
		}
	}
}
