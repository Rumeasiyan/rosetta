package api

import (
	"context"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/Rumeasiyan/rosetta"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
)

// The grammar paths (R-GRM-01; B 2): LanguageTool's public v2 form, so the editor's stock
// plug-in works once pointed here (docs/plugin-findings.md). A field past what the form has
// is an addition its parse ignores.
const (
	GrammarPrefix = "/api/v2/"
	checkPath     = GrammarPrefix + "check"
	languagesPath = GrammarPrefix + "languages"
)

// GrammarChecker is the check.
type GrammarChecker interface {
	Check(ctx context.Context, q grammar.Request) (grammar.Answer, *reasons.Failure)
}

type ltLanguage struct {
	Name     string `json:"name"`
	Code     string `json:"code"`
	LongCode string `json:"longCode"`
	Offered  bool   `json:"offered"`
}

// languageList is what the plug-in shows. Sinhala and Tamil are listed as not offered, and
// refused if chosen (R-GRM-02).
var languageList = []ltLanguage{
	{Name: "English", Code: "en", LongCode: "en-GB", Offered: true},
	{Name: "Sinhala (not offered)", Code: "si", LongCode: "si-LK"},
	{Name: "Tamil (not offered)", Code: "ta", LongCode: "ta-LK"},
}

func (s *Server) languages(w http.ResponseWriter, _ *http.Request) { writeJSON(w, languageList) }

// parseCheck reads the form. A form that will not parse can quote what it read, so the error
// is dropped, never logged or answered. enabledOnly is sent by the plug-in and means nothing
// here, as ROSETTA has no rules to enable.
func parseCheck(r *http.Request, ref string, body []byte) (grammar.Request, *reasons.Failure) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		return grammar.Request{}, reasons.New(reasons.Invalid)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil || !form.Has("text") {
		return grammar.Request{}, reasons.New(reasons.Invalid)
	}
	return grammar.Request{Ref: ref, Language: form.Get("language"), Text: form.Get("text")}, nil
}

type ltCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ltRule struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	IssueType   string     `json:"issueType"`
	Category    ltCategory `json:"category"`
}

type ltContext struct {
	Text   string `json:"text"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type ltReplacement struct {
	Value string `json:"value"`
}

// ltMatch is one suggestion. source is R-GRM-03's, an addition to the form.
type ltMatch struct {
	Message      string          `json:"message"`
	ShortMessage string          `json:"shortMessage"`
	Offset       int             `json:"offset"`
	Length       int             `json:"length"`
	Replacements []ltReplacement `json:"replacements"`
	Context      ltContext       `json:"context"`
	Rule         ltRule          `json:"rule"`
	Source       string          `json:"source"`
}

type ltSoftware struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
}

type ltDetected struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

// ltAnswerLanguage is always English. The plug-in looks its name up in the list above.
type ltAnswerLanguage struct {
	Name     string     `json:"name"`
	Code     string     `json:"code"`
	Detected ltDetected `json:"detectedLanguage"`
}

// checkAnswer is the answer. request_ref, fallback_used and sources are additions: R-PRV-03
// and R-TRN-06's reference, in a form that has no place for them.
type checkAnswer struct {
	Software   ltSoftware       `json:"software"`
	Language   ltAnswerLanguage `json:"language"`
	Matches    []ltMatch        `json:"matches"`
	RequestRef string           `json:"request_ref"`
	Fallback   bool             `json:"fallback_used"`
	Sources    []sourceAnswer   `json:"sources"`
}

// issueTypes is LanguageTool's word for each category.
var issueTypes = map[string]string{
	grammar.CategorySpelling: "misspelling", grammar.CategoryGrammar: "grammar", grammar.CategoryPunctuation: "typographical",
	grammar.CategoryStyle: "style", grammar.CategoryOther: "other",
}

// shapeCheck builds the form's answer. Each context comes from the text as sent, here, never
// from anything held.
func shapeCheck(ref, text string, a grammar.Answer) checkAnswer {
	out := checkAnswer{Software: ltSoftware{Name: "ROSETTA", Version: rosetta.Version, APIVersion: 1},
		Language:   ltAnswerLanguage{Name: "English", Code: "en", Detected: ltDetected{Name: "English", Code: "en"}},
		Matches:    make([]ltMatch, len(a.Suggestions)),
		RequestRef: ref, Fallback: a.Fallback, Sources: make([]sourceAnswer, len(a.Sources))}
	for i, u := range a.Sources {
		out.Sources[i] = sourceAnswer{Name: u.Name, Version: u.Version}
	}
	encoded := grammar.Encode(text)
	for i, g := range a.Suggestions {
		x := encoded.ContextOf(g)
		name := strings.ToUpper(g.Category)
		m := ltMatch{Message: g.Message, ShortMessage: "", Offset: g.Offset, Length: g.Length, Replacements: make([]ltReplacement, len(g.Replacements)),
			Context: ltContext{Text: x.Text, Offset: x.Offset, Length: x.Length}, Source: g.Source,
			Rule: ltRule{ID: "ROSETTA_" + name, Description: g.Category, IssueType: issueTypes[g.Category], Category: ltCategory{ID: name, Name: g.Category}}}
		for j, v := range g.Replacements {
			m.Replacements[j] = ltReplacement{Value: v}
		}
		out.Matches[i] = m
	}
	return out
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	if s.deps.Grammar == nil {
		s.refuse(w, r, reasons.New(reasons.NoProvider))
		return
	}
	req, fail := parseCheck(r, q.Ref, q.Body)
	if fail == nil {
		var a grammar.Answer
		if a, fail = s.deps.Grammar.Check(r.Context(), req); fail == nil {
			writeJSON(w, shapeCheck(q.Ref, req.Text, a))
			return
		}
	}
	s.deps.Logger.Info("a check was not answered", "request_ref", q.Ref, "system", q.Caller.Reference, "reason", string(fail.Reason))
	s.refuse(w, r, fail)
}
