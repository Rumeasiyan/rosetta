package api

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta"
	"github.com/Rumeasiyan/rosetta/internal/callers"
	"github.com/Rumeasiyan/rosetta/internal/grammar"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/tva"
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

// GrammarUse is one check, for the usage record E-REC-02 writes (R-REC-01). It holds no
// text: what R-REC-01 asks for, as far as a check has it.
type GrammarUse struct {
	Ref        string
	System     string // TVA's reference of the system it counts to
	CallerID   int64
	Officer    string // who is signed in; "" for a call with a key
	Characters int
	Sources    []grammar.Used
	Fallback   bool
	Duration   time.Duration
	Outcome    string // "answered", or the reason
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	q := requestOf(r)
	started := s.deps.Now()
	use := GrammarUse{Ref: q.Ref, System: q.Caller.Reference, CallerID: q.Caller.ID, Officer: q.Officer, Outcome: "answered"}
	if s.deps.Usage != nil {
		defer func() {
			use.Duration = s.deps.Now().Sub(started)
			s.deps.Usage(context.WithoutCancel(r.Context()), use)
		}()
	}
	var fail *reasons.Failure
	if s.deps.Grammar == nil {
		fail = reasons.New(reasons.NoProvider)
	} else {
		var req grammar.Request
		if req, fail = parseCheck(r, q.Ref, q.Body); fail == nil {
			use.Characters = utf8.RuneCountInString(req.Text)
			var a grammar.Answer
			if a, fail = s.deps.Grammar.Check(r.Context(), req); fail == nil {
				use.Sources, use.Fallback = a.Sources, a.Fallback
				writeJSON(w, shapeCheck(q.Ref, req.Text, a))
				return
			}
		}
	}
	use.Outcome = string(fail.Reason)
	s.deps.Logger.Info("a check was not answered", "request_ref", q.Ref, "system", q.Caller.Reference, "reason", string(fail.Reason))
	s.refuse(w, r, fail)
}

// The browser's way in (R-CAL-06; B 2, B 9).

// accountHeader names who is signed in. Only Caddy sets it, from CERBERUS's answer, and it
// strips any a visitor wrote (allspark's Caddyfile).
const accountHeader = "X-Cerberus-Account"

// maxAccount is the longest account name believed: past this it is nobody's.
const maxAccount = 256

// preflightSeconds is how long a browser may keep a preflight's answer.
const preflightSeconds = "600"

// Origins is who lists a browser's address.
type Origins interface {
	Find(ctx context.Context, origin string) (callers.Caller, error)
	Used(ctx context.Context, callerID int64) error
}

// plainWord is the message for a browser's refusal, where the reason's own speaks of
// translation or of a key.
func plainWord(r reasons.Reason) string {
	switch r {
	case reasons.NoKey:
		return "grammarNotSignedIn"
	case reasons.NoProvider:
		return "grammarNoProvider"
	case reasons.TimedOut:
		return "grammarTimedOut"
	}
	return r.Word()
}

// answerText answers a browser in plain text in the reader's language, English if it is not
// known, and counts it. The body is the fixed message and never anything sent.
func (s *Server) answerText(w http.ResponseWriter, r *http.Request, ref string, f *reasons.Failure, word string) {
	s.counts.Add(f.Reason)
	lang := i18n.Choose("", i18n.Accepted(r.Header.Get("Accept-Language")))
	w.Header().Add("Vary", "Accept-Language")
	reasons.WriteText(w, ref, f, i18n.T(lang, word), s.deps.Now())
}

// allowOrigin names the one address on the answer, never *, with credentials allowed.
func allowOrigin(h http.Header, origin string) {
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Credentials", "true")
	h.Set("Access-Control-Expose-Headers", "Retry-After, X-Rosetta-Reason, X-Rosetta-Request-Ref")
}

// listed finds the system listing origin that may still call. It says false for an address
// not listed, and for a fault gives the failure too.
func (s *Server) listed(ctx context.Context, origin string) (callers.Caller, bool, *reasons.Failure) {
	if s.deps.Origins == nil {
		return callers.Caller{}, false, nil
	}
	c, err := s.deps.Origins.Find(ctx, origin)
	switch {
	case errors.Is(err, callers.ErrNoSuchOrigin):
		return callers.Caller{}, false, nil
	case err != nil:
		s.deps.Logger.Error("a browser's address could not be checked")
		return callers.Caller{}, false, reasons.New(reasons.NoProvider)
	}
	return c, tva.MayCall(c.TVAStatus), nil
}

// grammarEntry decides whose call this is. A preflight is answered with no session. An
// origin not listed gets no CORS headers and a refusal, whatever else it holds. A key goes
// by the key's rules. Otherwise this is a browser's call: from a listed origin, with someone
// signed in, counted to the system that lists the origin.
func (s *Server) grammarEntry(next http.Handler) http.Handler {
	keyed := s.withKey(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		h := w.Header()
		h.Add("Vary", "Origin")
		ref := NewRef()
		origin := r.Header.Get("Origin")
		var owner callers.Caller
		ok := false
		if origin != "" {
			var fail *reasons.Failure
			if owner, ok, fail = s.listed(ctx, origin); fail != nil {
				s.answerText(w, r, ref, fail, plainWord(fail.Reason))
				return
			}
			if ok {
				allowOrigin(h, origin)
			}
		}
		if r.Method == http.MethodOptions {
			if !ok {
				s.answerText(w, r, ref, reasons.New(reasons.NoKey), "grammarNotListed")
				return
			}
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type")
			h.Set("Access-Control-Max-Age", preflightSeconds)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if origin != "" && !ok {
			s.answerText(w, r, ref, reasons.New(reasons.NoKey), "grammarNotListed")
			s.keepDenial(r, ref, owner.Reference, "The browser's address is not listed")
			return
		}
		if r.Header.Get("Authorization") != "" {
			keyed.ServeHTTP(w, r)
			return
		}
		if origin == "" {
			s.answerText(w, r, ref, reasons.New(reasons.NoKey), "grammarNotListed")
			s.keepDenial(r, ref, "", "No key and no browser address")
			return
		}
		account := strings.TrimSpace(r.Header.Get(accountHeader))
		if account == "" || len(account) > maxAccount {
			s.answerText(w, r, ref, reasons.New(reasons.NoKey), "grammarNotSignedIn")
			s.keepDenial(r, ref, owner.Reference, "Nobody is signed in")
			return
		}
		q := &Request{Ref: ref, Caller: owner, Browser: true, Officer: account}
		r = r.WithContext(context.WithValue(ctx, requestKey{}, q))
		body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
		switch {
		case err != nil:
			s.refuse(w, r, reasons.New(reasons.Invalid))
			return
		case len(body) > MaxBody:
			s.refuse(w, r, reasons.New(reasons.TooLong))
			return
		}
		q.Body = body
		if err := s.deps.Origins.Used(ctx, owner.ID); err != nil {
			s.deps.Logger.Error("a system's use was not noted", "request_ref", ref)
		}
		release, wait, allowed := s.limiter.Acquire(owner.ID, owner.PerMinute, owner.AtOnce)
		if !allowed {
			s.refuse(w, r, reasons.New(reasons.OverLimit).Until(s.deps.Now().Add(wait)))
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}
