// Package grammar is ROSETTA's grammar check (R-GRM): English text in, suggestions out, with
// positions true to the text the caller sent. The text is masked whatever it holds
// (R-MSK-02), split only as far as the sources need (R-PRV-04), and the suggestions joined.
//
// Grammar text lives only in this request's memory. It is never written to disk, a record,
// the cache, a log or an error (R-GRM-05; AGENTS C4): a failure is a reason, and logs carry
// the request reference.
package grammar

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/translate"
)

// The categories CALLERS.md gives. Any other reads as other.
const (
	CategorySpelling    = "spelling"
	CategoryGrammar     = "grammar"
	CategoryPunctuation = "punctuation"
	CategoryStyle       = "style"
	CategoryOther       = "other"
)

// maxReplacements is the most a suggestion gives (R-GRM-03).
const maxReplacements = 3

// Sources is where the text goes.
type Sources interface {
	Grammar(ctx context.Context, p sources.GrammarPiece) (sources.GrammarResult, error)
	MaxText() int
}

// Service is the check.
type Service struct {
	Sources  Sources
	Settings func(ctx context.Context) settings.Values
	// Held spares sources an identical text (R-GRM-05); nil holds nothing.
	Held   *Held
	Logger *slog.Logger
}

// Request is one check.
type Request struct {
	Ref      string
	Language string
	Text     string
}

// Suggestion is one finding, placed in the text the caller sent: Offset and Length are in
// UTF-16 units of it (R-GRM-03).
type Suggestion struct {
	Offset, Length int
	Message        string
	Replacements   []string
	Category       string
	Source         string
}

// Used is a source that answered.
type Used struct {
	Name, Version string
}

// Answer is the suggestions, in the order of the text, an empty list when there are none.
type Answer struct {
	Suggestions []Suggestion
	Sources     []Used
	Fallback    bool
}

var english = regexp.MustCompile(`^en(-[a-z0-9]{1,8})*$`)

// CheckLanguage reads the language a caller named. auto and any en or en-* are English
// (docs/plugin-findings.md); ROSETTA detects nothing and sends only en. Anything else is not
// offered, and no language at all is a request it cannot read.
func CheckLanguage(language string) *reasons.Failure {
	l := strings.ToLower(strings.TrimSpace(language))
	switch {
	case l == "":
		return reasons.New(reasons.Invalid)
	case l == "auto", english.MatchString(l):
		return nil
	}
	return reasons.New(reasons.Unsupported)
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Check answers one request, whole or not at all. The failure holds no text.
func (s *Service) Check(ctx context.Context, q Request) (Answer, *reasons.Failure) {
	if f := CheckLanguage(q.Language); f != nil {
		return Answer{}, f
	}
	v := s.Settings(ctx)
	if !utf8.ValidString(q.Text) {
		return Answer{}, reasons.New(reasons.Invalid)
	}
	// Code points here, as BUMBLEBEE counts; positions are UTF-16 (E-PRV-01's note).
	if utf8.RuneCountInString(q.Text) > v.Of(settings.GrammarCharsMax) {
		return Answer{}, reasons.New(reasons.TooLong)
	}
	if s.Held != nil {
		if a, ok := s.Held.Get(q.Text, keepFor(v)); ok {
			return a, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(v.Of(settings.RequestLimitSeconds))*time.Second)
	defer cancel()
	a, fail := s.run(ctx, q)
	if fail == nil && s.Held != nil {
		s.Held.Put(q.Text, a)
	}
	return a, fail
}

func (s *Service) run(ctx context.Context, q Request) (Answer, *reasons.Failure) {
	out := Answer{Suggestions: []Suggestion{}}
	m := mask.Mask(q.Text)
	for _, sp := range Chunks(m.Text, s.Sources.MaxText()) {
		if f := s.part(ctx, q, m, sp, true, &out); f != nil {
			return Answer{}, f
		}
	}
	out.Suggestions = inOrder(out.Suggestions)
	return out, nil
}

// part sends one piece and places what comes back. On "too long" the list has been read
// again: the piece is split to the new size and each part sent, once (R-PRV-04).
func (s *Service) part(ctx context.Context, q Request, m mask.Masked, sp translate.Span, again bool, out *Answer) *reasons.Failure {
	piece := m.Slice(sp.Start, sp.End)
	if !strings.ContainsFunc(mask.WithoutStandIns(piece.Text), unicode.IsLetter) {
		return nil // nothing here for a source to read
	}
	r, err := s.Sources.Grammar(ctx, sources.GrammarPiece{Ref: q.Ref, Text: piece.Text})
	var tl *sources.TooLong
	if again && errors.As(err, &tl) {
		for _, inner := range Chunks(m.Text[sp.Start:sp.End], tl.Limit) {
			inner = translate.Span{Start: sp.Start + inner.Start, End: sp.Start + inner.End}
			if f := s.part(ctx, q, m, inner, false, out); f != nil {
				return f
			}
		}
		return nil
	}
	if err != nil {
		return s.failure(q, err)
	}
	// Where this piece begins in the text sent. A piece never begins inside a stand-in.
	base, ok := m.ToOriginal(mask.Units(m.Text[:sp.Start]))
	if !ok {
		return nil
	}
	for _, found := range r.Suggestions {
		// A message holding a stand-in would show the reader the mask, not their words.
		if placed, ok := place(piece, base, found.Start, found.Length); ok && mask.WithoutStandIns(found.Message) == found.Message {
			out.Suggestions = append(out.Suggestions, Suggestion{Offset: placed.offset, Length: placed.length, Message: found.Message,
				Replacements: replacements(found.Replacements), Category: category(found.Category), Source: r.Source})
		}
	}
	if u := (Used{r.Source, r.Version}); !slices.Contains(out.Sources, u) {
		out.Sources = append(out.Sources, u)
	}
	out.Fallback = out.Fallback || r.Fallback
	return nil
}

type position struct{ offset, length int }

// place finds a suggestion in the text sent. It says false for one that cannot be placed
// exactly, which is dropped, never moved (R-GRM-03): one outside the piece, one with no
// length, one that begins or ends between the two units of a character, and one touching a
// stand-in, which is about a number the source never saw.
func place(piece mask.Masked, base, start, length int) (position, bool) {
	end := start + length
	sent := utf16.Encode([]rune(piece.Text))
	if start < 0 || length <= 0 || end > len(sent) || betweenUnits(sent, start) || betweenUnits(sent, end) {
		return position{}, false
	}
	for _, it := range piece.Items {
		from := mask.Units(piece.Text[:it.Start])
		if start < from+mask.Units(it.Token) && from < end {
			return position{}, false
		}
	}
	from, ok := piece.ToOriginal(start)
	if !ok {
		return position{}, false
	}
	to, ok := piece.ToOriginal(end)
	if !ok {
		return position{}, false
	}
	return position{base + from, to - from}, true
}

// betweenUnits says a position falls inside a character of two units.
func betweenUnits(text []uint16, at int) bool {
	return at > 0 && at < len(text) && lowSurrogate(text[at]) && highSurrogate(text[at-1])
}

func highSurrogate(u uint16) bool { return u >= 0xD800 && u < 0xDC00 }
func lowSurrogate(u uint16) bool  { return u >= 0xDC00 && u < 0xE000 }

// replacements is at most three, as a list and never nil, as the plug-in slices it. A
// replacement holding a stand-in would write the mask into the letter, so it goes.
func replacements(list []string) []string {
	out := make([]string, 0, min(len(list), maxReplacements))
	for _, r := range list {
		if len(out) < maxReplacements && mask.WithoutStandIns(r) == r {
			out = append(out, r)
		}
	}
	return out
}

// inOrder is the suggestions by place in the text, each kept only if it does not overlap one
// before it. The plug-in marks them one after another in the text, so one overlapping another
// could only be shown in the wrong place: it is dropped, never moved.
func inOrder(list []Suggestion) []Suggestion {
	slices.SortStableFunc(list, func(a, b Suggestion) int { return cmp.Compare(a.Offset, b.Offset) })
	out := list[:0]
	end := 0
	for _, s := range list {
		if s.Offset >= end {
			out = append(out, s)
			end = s.Offset + s.Length
		}
	}
	return out
}

func category(c string) string {
	switch c {
	case CategorySpelling, CategoryGrammar, CategoryPunctuation, CategoryStyle:
		return c
	}
	return CategoryOther
}

// failure turns a source's error into the one reason the caller is given.
func (s *Service) failure(q Request, err error) *reasons.Failure {
	var ex *sources.Exhausted
	var tl *sources.TooLong
	switch {
	case errors.As(err, &ex):
		s.log().Warn("no grammar source answered", "request_ref", q.Ref)
		return reasons.New(reasons.NoProvider).Until(ex.RetryAt)
	case errors.As(err, &tl):
		return reasons.New(reasons.TooLong)
	case errors.Is(err, sources.ErrTooLate), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return reasons.New(reasons.TimedOut)
	}
	s.log().Error("a grammar call ended with no reason", "request_ref", q.Ref)
	return reasons.New(reasons.NoProvider)
}

// Chunks is the text in pieces no longer than limit code points (R-GRM-02), broken where
// translate.Split breaks it and put back together as far as limit allows: a source reads a
// sentence better among its neighbours, and a short letter costs one call, not one a sentence.
func Chunks(text string, limit int) []translate.Span {
	var out []translate.Span
	for _, sp := range translate.Split(text, limit) {
		if n := len(out) - 1; n >= 0 && utf8.RuneCountInString(text[out[n].Start:sp.End]) <= limit {
			out[n].End = sp.End
			continue
		}
		out = append(out, sp)
	}
	return out
}

// contextReach is how much of the text a suggestion's context shows either side.
const contextReach = 40

// Context is a little of the text around a suggestion, for the panel. Offset and Length are
// in UTF-16 units of Text. It is made fresh for each answer from the text the caller sent,
// which is why nothing held needs any of the text (R-GRM-05).
type Context struct {
	Text           string
	Offset, Length int
}

// Encoded is a text in the units the editor counts.
type Encoded []uint16

// Encode reads a text into UTF-16.
func Encode(text string) Encoded { return utf16.Encode([]rune(text)) }

// ContextOf is the context of a suggestion placed in this text. It never cuts a character
// made of two units.
func (e Encoded) ContextOf(s Suggestion) Context {
	from := max(s.Offset-contextReach, 0)
	if from > 0 && lowSurrogate(e[from]) {
		from--
	}
	to := min(s.Offset+s.Length+contextReach, len(e))
	if to < len(e) && lowSurrogate(e[to]) {
		to++
	}
	return Context{Text: string(utf16.Decode(e[from:to])), Offset: s.Offset - from, Length: s.Length}
}
