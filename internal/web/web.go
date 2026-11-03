// Package web renders ROSETTA's screens: Go templates and htmx in the look the
// owner approved in the design lab ("Stone bands", docs/DECISION-LOG.md), in the
// person's language (R-NFR-05). Fonts and scripts come from ROSETTA itself, never
// from outside (R-NFR-11).
package web

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Rumeasiyan/rosetta"
	"github.com/Rumeasiyan/rosetta/internal/glossary"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/memory"
	"github.com/Rumeasiyan/rosetta/internal/people"
)

//go:embed pages/*.html
var pageFiles embed.FS

//go:embed static
var staticFiles embed.FS

// StaticPrefix is where ROSETTA's own files are served. It carries the version, so
// browsers keep them for a year and a release is a new path.
var StaticPrefix = "/static/" + rosetta.Version + "/"

// LangCookie holds the language picked with the switch.
const LangCookie = "rosetta_lang"

// Viewer is the signed-in person as the screens see them.
type Viewer struct {
	Name  string
	Roles []string // message keys
}

// NavItem is one entry of the top bar.
type NavItem struct {
	ID   string // matches Page.Here
	Word string // message key
	Href string
}

// colombo is Sri Lanka time: times are stored in UTC and shown in Sri Lanka time.
var colombo = func() *time.Location {
	l, err := time.LoadLocation("Asia/Colombo")
	if err != nil {
		return time.FixedZone("Asia/Colombo", 5*3600+1800)
	}
	return l
}()

// When shows a time in Sri Lanka time, to the minute.
func When(t any) string {
	switch v := t.(type) {
	case time.Time:
		return v.In(colombo).Format("2006-01-02 15:04")
	case *time.Time:
		if v != nil {
			return v.In(colombo).Format("2006-01-02 15:04")
		}
	}
	return ""
}

// Day shows a date as it was stored.
func Day(d *time.Time) string {
	if d == nil {
		return ""
	}
	return d.Format(time.DateOnly)
}

// anyDay shows a date whether a screen holds it directly or as an optional one.
func anyDay(d any) string {
	switch v := d.(type) {
	case time.Time:
		return Day(&v)
	case *time.Time:
		return Day(v)
	}
	return ""
}

// RoleWord is the message key for a stored role name.
func RoleWord(role string) string {
	switch role {
	case people.Administrator:
		return "roleAdministrator"
	case people.GlossaryKeeper:
		return "roleGlossaryKeeper"
	case people.MemoryReviewer:
		return "roleMemoryReviewer"
	}
	return role
}

// entryTable is what the shared table of glossary entries takes.
type entryTable struct {
	Lang    i18n.Lang
	Entries []glossary.Entry
}

func tableArgs(l i18n.Lang, entries []glossary.Entry) entryTable { return entryTable{l, entries} }

// memoryTable is what the shared table of memory entries takes.
type memoryTable struct {
	Lang    i18n.Lang
	Entries []memory.Entry
}

func memoryTableArgs(l i18n.Lang, entries []memory.Entry) memoryTable { return memoryTable{l, entries} }

func memoryStateWord(state string) string {
	switch state {
	case memory.StateServed:
		return "memoryStateServed"
	case memory.StateRecheck:
		return "memoryStateRecheck"
	case memory.StateRemoved:
		return "memoryStateRemoved"
	case memory.StateReplaced:
		return "memoryStateReplaced"
	}
	return state
}

// Page is what every screen's template receives.
type Page struct {
	Lang      i18n.Lang
	Path      string // this page's path
	Title     string // message key of the page title
	Here      string // the top bar entry that is current
	Nav       []NavItem
	Viewer    *Viewer
	CSRF      string
	Flash     string // message key, or ""
	FlashTone string // good, warn or bad
	Next      string // where a sign-in returns to
	Data      any
}

// Renderer holds the parsed screens.
type Renderer struct {
	pages map[string]*template.Template
	// signOutOrigins are added to form-action on a page holding the sign-out form.
	signOutOrigins []string
	logger         *slog.Logger
}

// NewRenderer parses every screen once, at start.
func NewRenderer(logger *slog.Logger) (*Renderer, error) {
	base, err := template.New("").Funcs(template.FuncMap{
		"t":               i18n.T,
		"langs":           func() []i18n.Lang { return i18n.Langs },
		"langName":        i18n.Name,
		"staticPath":      func(name string) string { return StaticPrefix + name },
		"when":            When,
		"day":             anyDay,
		"roleWord":        RoleWord,
		"flagWord":        flagWord,
		"stateWord":       stateWord,
		"direction":       directionName,
		"typeWord":        typeWord,
		"statusWord":      statusWord,
		"args":            tableArgs,
		"memoryArgs":      memoryTableArgs,
		"memoryStateWord": memoryStateWord,
		"opWord":          opWord,
		"alertWord":       alertWord,
		"alertAbout":      alertAbout,
		"flagLanguage":    func(_ i18n.Lang, code string) string { l, _ := i18n.Parse(code); return i18n.Name(l) },
		"sensitivities":   func() []string { return testSensitivities },
		"sensitivityWord": sensitivityWord,
		"whyWord":         whyWord,
		"timeWord":        timeWord,
		"groupWord":       groupWord,
	}).ParseFS(pageFiles, "pages/layout.html")
	if err != nil {
		return nil, err
	}
	s := &Renderer{pages: map[string]*template.Template{}, logger: logger}
	names, err := fs.Glob(pageFiles, "pages/*.html")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		name := strings.TrimSuffix(strings.TrimPrefix(n, "pages/"), ".html")
		if name == "layout" {
			continue
		}
		clone, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if s.pages[name], err = clone.ParseFS(pageFiles, n); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Render writes a screen with status. It renders into a buffer first, so a
// template error never sends half a page.
func (s *Renderer) Render(w http.ResponseWriter, r *http.Request, status int, name string, p Page) {
	t, ok := s.pages[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "page", p); err != nil {
		s.logger.Error("render", "page", name, "error", err)
		http.Error(w, i18n.T(p.Lang, "somethingWrong"), http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Vary", "Cookie, Accept-Language")
	// Only a page with the sign-out form on it may post anywhere but here.
	if p.Viewer != nil {
		SecurityHeaders(h, s.signOutOrigins...)
	} else {
		SecurityHeaders(h)
	}
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// SecurityHeaders are set on every screen: nothing loads from elsewhere, no inline
// script or style, and no other site may frame ROSETTA. Every form posts back to
// ROSETTA, and forms go nowhere else: the origins given are added to form-action alone,
// since Chrome also holds a redirect after a form's post to it. Sign-in is a link, not a
// form, so it needs none of this.
func SecurityHeaders(h http.Header, formOrigins ...string) {
	action := "'self'"
	if len(formOrigins) > 0 {
		action += " " + strings.Join(formOrigins, " ")
	}
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'; form-action "+action+"; frame-ancestors 'none'; base-uri 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
}

// originsOf is the origin of each address, each once, in order. An address with no
// origin is left out, so nothing but scheme and host can reach the header.
func originsOf(addresses ...string) []string {
	var out []string
	for _, a := range addresses {
		u, err := url.Parse(a)
		if err != nil || u.Scheme == "" || u.Host == "" {
			continue
		}
		if origin := u.Scheme + "://" + u.Host; !slices.Contains(out, origin) {
			out = append(out, origin)
		}
	}
	return out
}

// Language is the language for this request: the person's pick, else the
// browser's, else English.
func Language(r *http.Request) i18n.Lang {
	var picked string
	if c, err := r.Cookie(LangCookie); err == nil {
		picked = c.Value
	}
	return i18n.Choose(picked, i18n.Accepted(r.Header.Get("Accept-Language")))
}

// SafeNext keeps a redirect inside the screens: a path here, never another site.
func SafeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return "/"
	}
	return next
}

// staticHandler serves ROSETTA's embedded files, never a folder list.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix(StaticPrefix, http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		files.ServeHTTP(w, r)
	})
}
