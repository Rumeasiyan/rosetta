package web

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/people"
)

// These need no database: they check what a screen sends for given data.

func render(t *testing.T, name string, p Page) string {
	t.Helper()
	r, err := NewRenderer(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	r.Render(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, name, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", name, rec.Code, rec.Body)
	}
	return rec.Body.String()
}

func someGrants() []people.Grant {
	at := time.Date(2030, 1, 1, 4, 30, 0, 0, time.UTC)
	end := time.Date(2030, 6, 30, 0, 0, 0, 0, time.UTC)
	return []people.Grant{
		{ID: 1, Name: "Test Administrator", Email: "dev-admin@example.invalid", Role: people.Administrator, PostRef: people.PostNotKnown, StartsAt: at},
		{ID: 2, Name: "Test Keeper", Email: "dev-keeper@example.invalid", Role: people.GlossaryKeeper, Language: i18n.TA, PostRef: people.PostNotKnown, StartsAt: at, EndsOn: &end},
	}
}

func TestPeopleScreenInEachLanguage(t *testing.T) {
	for _, l := range i18n.Langs {
		body := render(t, "people", Page{Lang: l, Path: "/people", Title: "people", Here: "people", CSRF: "form-token",
			Nav:    []NavItem{{ID: "people", Word: "people", Href: "/people"}},
			Viewer: &Viewer{Name: "Test Administrator"},
			Data:   peopleData{Grants: someGrants(), Roles: grantableRoles}})
		for _, want := range []string{
			`<html lang="` + string(l) + `">`,
			`aria-current="page"`,
			i18n.T(l, "grantTitle"),
			i18n.T(l, "roleGlossaryKeeper"),
			`action="/people/2/end"`,
			`name="csrf" value="form-token"`,
			`scope="col"`,
			`2030-01-01 10:00`, // Sri Lanka time
			`2030-06-30`,
			`lang="ta"`,
			`<label for="email">`,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", l, want)
			}
		}
		// Every language is on the switch, each named in its own script.
		for _, other := range i18n.Langs {
			if !strings.Contains(body, `href="/lang/`+string(other)+`?next=%2fpeople"`) || !strings.Contains(body, i18n.Name(other)) {
				t.Errorf("%s: the switch has no %s", l, other)
			}
		}
		if strings.Contains(body, `style="`) || strings.Contains(body, "<style") || strings.Contains(body, "onclick") {
			t.Errorf("%s: inline style or script, which the policy forbids", l)
		}
	}
}

func TestHomeShowsRolesOrSaysThereAreNone(t *testing.T) {
	body := render(t, "home", Page{Lang: i18n.EN, Path: "/", Title: "welcome", Viewer: &Viewer{Name: "Test Officer"}, Data: homeData{}})
	if !strings.Contains(body, i18n.T(i18n.EN, "noRole")) || strings.Contains(body, i18n.T(i18n.EN, "yourRoles")) {
		t.Fatal("no role should say so")
	}
	body = render(t, "home", Page{Lang: i18n.EN, Path: "/", Title: "welcome",
		Viewer: &Viewer{Name: "Test Owner", Roles: []string{"roleSystemOwner"}},
		Data:   homeData{Owned: []people.OwnedSystem{{Reference: "TEST-0001", Name: "Test letter editor"}}}})
	if !strings.Contains(body, "Test letter editor (TEST-0001)") || !strings.Contains(body, i18n.T(i18n.EN, "roleSystemOwner")) {
		t.Fatal(body)
	}
}

func TestNavFollowsTheRole(t *testing.T) {
	admin := &current{grants: []people.Grant{{Role: people.Administrator}}}
	keeper := &current{grants: []people.Grant{{Role: people.GlossaryKeeper}}}
	if !isAdmin(admin) || isAdmin(keeper) || isAdmin(&current{}) {
		t.Fatal("isAdmin")
	}
	if len(nav(admin)) == 0 || len(nav(keeper)) != 0 || len(nav(&current{})) != 0 {
		t.Fatal("the bar offers screens the role does not open")
	}
	owner := &current{owned: []people.OwnedSystem{{Reference: "TEST-0001"}}}
	if isAdmin(owner) {
		t.Fatal("owning a system is not administering")
	}
	if v := owner.viewer(); len(v.Roles) != 1 || v.Roles[0] != "roleSystemOwner" {
		t.Fatalf("%+v", v)
	}
	if a := (&current{sess: session{personID: 1, subject: "dev-admin"}}).actor(); a.Post != "not known" || a.Subject != "dev-admin" {
		t.Fatalf("%+v", a)
	}
}

func TestStyleSheetStacksTablesAndKeepsTargets(t *testing.T) {
	css, err := os.ReadFile("static/rosetta.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"@container (max-width: 600px)", "min-height: 44px", ":focus-visible", "Noto Serif Sinhala", "Noto Serif Tamil", "Abril Fatface"} {
		if !strings.Contains(string(css), want) {
			t.Errorf("no %q", want)
		}
	}
}

// Roles are held by a few named people (s2): many more than that still fits
// R-NFR-03, and the form sends no list of everyone who has signed in.
func TestALongPageStaysUnderTheLimit(t *testing.T) {
	var gs []people.Grant
	for range 100 {
		gs = append(gs, someGrants()...)
	}
	body := render(t, "people", Page{Lang: i18n.SI, Path: "/people", Title: "people", Viewer: &Viewer{Name: "Test"}, Data: peopleData{Grants: gs, Roles: grantableRoles}})
	if len(body) > 200<<10 {
		t.Fatalf("%d bytes", len(body))
	}
}
