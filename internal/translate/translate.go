// Package translate is ROSETTA's piece pipeline (R-TRN-04; B 3): a list of passages in,
// the same number out in the same order, or one failure with the passage's position and
// reason (R-TRN-02). Each passage is masked, split into pieces, and each piece answered
// in order from the glossary, the memory, the cache and then a source. Later stories
// hook into this: the glossary and the memory answer nothing yet.
//
// Caller text lives only in this request's memory. It never reaches a log, a record, an
// error or the outbox (AGENTS C4): a failure is a reason and a position, and logs carry
// the request reference.
package translate

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
)

// Where a piece came from (R-TRN-06).
const (
	OriginGlossary = "glossary"
	OriginMemory   = "memory"
	OriginCache    = "cache"
	OriginSource   = "source"
)

// Request is a list of passages to translate.
type Request struct {
	Ref         string
	From, To    string
	Sensitivity string
	Passages    []string
}

// Lookup is one piece as an Answerer sees it.
type Lookup struct {
	From, To    string
	Sensitivity string
	Text        string // masked, numbered from one, as a source would see it
	Normal      string // Text, normalised: what to compare and to hash (R-NFR-05)
	Plain       string // the piece as the caller sent it, values and all
	Allowed     []bumblebee.Source
}

// Hit is a piece answered without a source call. Its text holds the same stand-ins as
// Lookup.Text, which are restored like any other answer's.
type Hit struct {
	Text    string
	Origin  string
	Source  string // for the cache: the source that made it
	Version string
}

// Answerer may know a piece already: the glossary (E-GLO-04), the memory (E-MEM-02) and
// the cache (E-TRN-03) are one each, asked in that order (R-TRN-04). An error is logged
// as fixed words and counts as not knowing.
type Answerer interface {
	Answer(ctx context.Context, l Lookup) (Hit, bool, error)
}

// None answers nothing: what the glossary and the memory do until R4.
type None struct{}

// Answer knows nothing.
func (None) Answer(context.Context, Lookup) (Hit, bool, error) { return Hit{}, false, nil }

// Terms finds glossary terms in a text, to help the translator when a request needs a
// person (R-GAT-03). E-GLO-04 gives the real one.
type Terms func(ctx context.Context, from string, passages []string) []string

// Sources is where pieces go that nothing else knows.
type Sources interface {
	Allowed(ctx context.Context, from, to, sensitivity string) ([]bumblebee.Source, bool)
	Translate(ctx context.Context, p sources.Piece) (sources.Result, error)
	MaxText() int
}

// Service is the pipeline.
type Service struct {
	Glossary Answerer
	Memory   Answerer
	Cache    Answerer
	Terms    Terms
	Sources  Sources
	Settings func(ctx context.Context) settings.Values
	Logger   *slog.Logger
	Now      func() time.Time // time.Now when nil
}

// PieceInfo says where one piece of a passage came from.
type PieceInfo struct {
	Origin   string
	Source   string
	Version  string
	Fallback bool
}

// Passage is one passage translated.
type Passage struct {
	Text   string
	Pieces []PieceInfo
}

// Answer is every passage, in order.
type Answer struct {
	Passages []Passage
	// Sensitivity is what the text was treated as: what was stated, or personal where
	// ordinary text held an identity card number (R-MSK-04).
	Sensitivity string
	Mismatch    bool
}

// piece is one piece on its way through.
type piece struct {
	passage int
	before  string // what lies between the last piece and this one, as it was
	masked  mask.Masked
	info    PieceInfo
	text    string // the finished piece, values restored
}

func validLanguage(s string) bool {
	_, ok := i18n.Parse(s)
	return ok && s == strings.ToLower(strings.TrimSpace(s))
}

// check refuses what cannot be taken, with nothing it holds in the failure. Too many or
// too much is "too long", with the position when one passage is at fault.
func check(q Request, v settings.Values) *reasons.Failure {
	switch {
	case !validLanguage(q.From), !validLanguage(q.To), q.From == q.To, len(q.Passages) == 0,
		q.Sensitivity != sources.Ordinary && q.Sensitivity != sources.Personal && q.Sensitivity != sources.Confidential:
		return reasons.New(reasons.Invalid)
	case len(q.Passages) > v.Of(settings.PassagesMax):
		return reasons.New(reasons.TooLong)
	}
	total := 0
	for i, p := range q.Passages {
		n := utf8.RuneCountInString(p)
		if n > v.Of(settings.PassageCharsMax) {
			return reasons.New(reasons.TooLong).At(i)
		}
		total += n
	}
	if total > v.Of(settings.RequestCharsMax) {
		return reasons.New(reasons.TooLong)
	}
	return nil
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Translate answers a request, whole or not at all. The failure is the one way out with
// no answer, and holds no text.
func (s *Service) Translate(ctx context.Context, q Request) (Answer, *reasons.Failure) {
	v := s.Settings(ctx)
	if f := check(q, v); f != nil {
		return Answer{}, f
	}
	// There are no providers on provincial machines yet, so nothing can be done with
	// confidential text, whatever it holds (R-MSK-01). It goes nowhere, not even the
	// glossary's look-up.
	if q.Sensitivity == sources.Confidential {
		return Answer{}, reasons.New(reasons.NoProvincial)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(v.Of(settings.RequestLimitSeconds))*time.Second)
	defer cancel()

	// Each passage is masked before it is split, so a number written with spaces can
	// never be broken up and sent in parts. The stand-ins and values stay here.
	limit := s.Sources.MaxText()
	var all []*piece
	tails := make([]string, len(q.Passages))
	mismatch := false
	for i, text := range q.Passages {
		m := mask.Mask(text)
		// Ordinary text holding an identity card number is treated as personal, the
		// whole request with it: another passage may well name the same person.
		if q.Sensitivity == sources.Ordinary && m.HasCard() {
			mismatch = true
		}
		last := 0
		for _, sp := range Split(m.Text, limit) {
			all = append(all, &piece{passage: i, before: m.Text[last:sp.Start], masked: m.Slice(sp.Start, sp.End)})
			last = sp.End
		}
		tails[i] = m.Text[last:]
	}

	if mismatch {
		q.Sensitivity = sources.Personal
		s.log().Info("ordinary text held an identity card number", "request_ref", q.Ref)
	}
	allowed, read := s.Sources.Allowed(ctx, q.From, q.To, q.Sensitivity)
	var need []*piece
	for _, p := range all {
		if f := s.known(ctx, q, p, allowed); f != nil {
			return Answer{}, f
		}
		if p.info.Origin == "" {
			need = append(need, p)
		}
	}
	// A direction closed for every allowed source needs a person (R-GAT-03). A list
	// never read says nothing of the gate: that is no provider, below.
	if len(need) > 0 && read && len(allowed) == 0 {
		f := reasons.New(reasons.HumanRequired).At(need[0].passage)
		if s.Terms != nil {
			f.Terms = s.Terms(ctx, q.From, q.Passages)
		}
		return Answer{}, f
	}
	// One piece twice in a request goes to a source once: what comes back is masked, so
	// each restores its own values. It lasts only as long as this request.
	done := map[string]*sourceAnswer{}
	for _, p := range need {
		if f := s.fromSource(ctx, q, p, done); f != nil {
			return Answer{}, f.At(p.passage)
		}
	}
	a := join(q, all, tails)
	a.Sensitivity, a.Mismatch = q.Sensitivity, mismatch
	return a, nil
}

// known asks the glossary, the memory and the cache, in that order (R-TRN-04).
func (s *Service) known(ctx context.Context, q Request, p *piece, allowed []bumblebee.Source) *reasons.Failure {
	l := Lookup{From: q.From, To: q.To, Sensitivity: q.Sensitivity, Text: p.masked.Text, Normal: Normalise(p.masked.Text), Plain: p.masked.Plain(), Allowed: allowed}
	for _, a := range []struct {
		name string
		it   Answerer
	}{{OriginGlossary, s.Glossary}, {OriginMemory, s.Memory}, {OriginCache, s.Cache}} {
		// Personal text is never cached (R-TRN-08), and not even looked for there: its
		// hash stays out of the database too.
		if a.it == nil || (a.name == OriginCache && q.Sensitivity != sources.Ordinary) {
			continue
		}
		hit, ok, err := a.it.Answer(ctx, l)
		if err != nil {
			s.log().Error("a piece could not be looked up", "request_ref", q.Ref, "from", a.name)
			continue
		}
		if !ok {
			continue
		}
		text, err := p.masked.Restore(hit.Text)
		if err != nil {
			return reasons.New(reasons.MaskingLost).At(p.passage)
		}
		p.text, p.info = text, PieceInfo{Origin: hit.Origin, Source: hit.Source, Version: hit.Version}
		return nil
	}
	return nil
}

// sourceAnswer is what a source gave for one masked text: one result, or the parts it
// had to be split into. Each is masked text, so a piece seen twice in a request restores
// its own values from it.
type sourceAnswer struct {
	result sources.Result
	spans  []Span // the parts, when it was split
	parts  []sources.Result
}

// fromSource translates one piece. On "too long" the list has been read again: the piece
// is split to the new size, and each part sent (R-PRV-04). A part too long again is a
// failure, since the list keeps changing under it.
func (s *Service) fromSource(ctx context.Context, q Request, p *piece, done map[string]*sourceAnswer) *reasons.Failure {
	got, ok := done[p.masked.Text]
	if !ok {
		send := func(text string) (sources.Result, error) {
			return s.Sources.Translate(ctx, sources.Piece{Ref: q.Ref, From: q.From, To: q.To, Sensitivity: q.Sensitivity, Text: text})
		}
		r, err := send(p.masked.Text)
		var tl *sources.TooLong
		switch {
		case err == nil:
			got = &sourceAnswer{result: r}
		case errors.As(err, &tl):
			got = &sourceAnswer{spans: Split(p.masked.Text, tl.Limit)}
			for _, sp := range got.spans {
				r, err := send(p.masked.Slice(sp.Start, sp.End).Text)
				if err != nil {
					return s.failure(q, err)
				}
				got.parts = append(got.parts, r)
			}
		default:
			return s.failure(q, err)
		}
		done[p.masked.Text] = got
	}
	if got.spans == nil {
		text, err := p.masked.Restore(got.result.Text)
		if err != nil {
			return s.lost(q)
		}
		p.text = text
		p.info = PieceInfo{Origin: OriginSource, Source: got.result.Source, Version: got.result.Version, Fallback: got.result.Fallback}
		return nil
	}
	var b strings.Builder
	last := 0
	for i, sp := range got.spans {
		text, err := p.masked.Slice(sp.Start, sp.End).Restore(got.parts[i].Text)
		if err != nil {
			return s.lost(q)
		}
		b.WriteString(p.masked.Text[last:sp.Start])
		b.WriteString(text)
		last = sp.End
		r := got.parts[i]
		p.info = PieceInfo{Origin: OriginSource, Source: r.Source, Version: r.Version, Fallback: p.info.Fallback || r.Fallback}
	}
	b.WriteString(p.masked.Text[last:])
	p.text = b.String()
	return nil
}

func (s *Service) lost(q Request) *reasons.Failure {
	s.log().Warn("a source lost or changed a stand-in", "request_ref", q.Ref)
	return reasons.New(reasons.MaskingLost)
}

// failure turns a source's error into the one reason the caller is given.
func (s *Service) failure(q Request, err error) *reasons.Failure {
	var ex *sources.Exhausted
	var tl *sources.TooLong
	switch {
	case errors.As(err, &ex):
		s.log().Warn("no source answered", "request_ref", q.Ref)
		return reasons.New(reasons.NoProvider).Until(ex.RetryAt)
	case errors.As(err, &tl):
		return reasons.New(reasons.TooLong)
	case errors.Is(err, sources.ErrTooLate), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return reasons.New(reasons.TimedOut)
	}
	s.log().Error("a source call ended with no reason", "request_ref", q.Ref)
	return reasons.New(reasons.NoProvider)
}

// join is each passage's pieces in their original order, with what lay between them and
// at the edges kept byte for byte (R-TRN-04). Stand-ins never lie between pieces: they
// are always inside one.
func join(q Request, all []*piece, tails []string) Answer {
	builders := make([]strings.Builder, len(q.Passages))
	out := Answer{Passages: make([]Passage, len(q.Passages))}
	for _, p := range all {
		b := &builders[p.passage]
		b.WriteString(p.before)
		b.WriteString(p.text)
		out.Passages[p.passage].Pieces = append(out.Passages[p.passage].Pieces, p.info)
	}
	for i := range q.Passages {
		builders[i].WriteString(tails[i])
		out.Passages[i].Text = builders[i].String()
	}
	return out
}
