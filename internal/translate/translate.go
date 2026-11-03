// Package translate is ROSETTA's piece pipeline (R-TRN-04; B 3): a list of passages in,
// the same number out in the same order, or one failure with the passage's position and
// reason (R-TRN-02). Each passage is masked, split into pieces, and each piece answered
// in order from the glossary, the memory, the cache and then a source. Glossary terms are
// protected with stand-ins of the masking family before a passage is split, so a source never sees
// them and each comes out as published (R-GLO-03).
//
// Caller text lives only in this request's memory. It never reaches a log, a record, an
// error or the outbox (AGENTS C4): a failure is a reason and a position, and logs carry
// the request reference.
package translate

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Rumeasiyan/rosetta/internal/bumblebee"
	"github.com/Rumeasiyan/rosetta/internal/i18n"
	"github.com/Rumeasiyan/rosetta/internal/mask"
	"github.com/Rumeasiyan/rosetta/internal/reasons"
	"github.com/Rumeasiyan/rosetta/internal/settings"
	"github.com/Rumeasiyan/rosetta/internal/sources"
	"github.com/Rumeasiyan/rosetta/internal/terms"
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
	// Tally, when set, is told what happened, whole or failed, for the usage record (R-REC-01).
	Tally *Tally
}

// Tally is what a request did, with none of its text: what was treated how, and the pieces
// answered before any failure.
type Tally struct {
	Sensitivity string // as treated: personal where ordinary text held a card (R-MSK-04)
	Mismatch    bool
	Pieces      map[string]int // by origin
	Sources     []string
	Fallback    bool
}

// add counts the pieces answered so far.
func (t *Tally) add(all []*piece) {
	t.Pieces = map[string]int{}
	for _, p := range all {
		if p.info.Origin == "" {
			continue
		}
		t.Pieces[p.info.Origin]++
		t.Fallback = t.Fallback || p.info.Fallback
		if p.info.Source != "" && !slices.Contains(t.Sources, p.info.Source) {
			t.Sources = append(t.Sources, p.info.Source)
		}
	}
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

// Answerer may know a piece already: the memory (E-MEM-02) and the cache (E-TRN-03) are one
// each, asked in that order after whole glossary terms (R-TRN-04). An error is logged as fixed
// words and counts as not knowing.
type Answerer interface {
	Answer(ctx context.Context, l Lookup) (Hit, bool, error)
}

// None answers nothing.
type None struct{}

// Answer knows nothing.
func (None) Answer(context.Context, Lookup) (Hit, bool, error) { return Hit{}, false, nil }

// Keeper holds a translated piece of ordinary text (R-TRN-08). A failure is logged as
// fixed words and changes no answer.
type Keeper interface {
	Keep(ctx context.Context, k Keep) error
}

// Glossary is the glossary as the pipeline needs it: the version now, and the terms of a direction.
// An error fails the request, as every answer must state the version it used (R-TRN-06).
type Glossary interface {
	Terms(ctx context.Context, from, to string) (int, *terms.Set, error)
}

// Sources is where pieces go that nothing else knows.
type Sources interface {
	Allowed(ctx context.Context, from, to, sensitivity string) ([]bumblebee.Source, bool)
	Translate(ctx context.Context, p sources.Piece) (sources.Result, error)
	MaxText() int
}

// Service is the pipeline.
type Service struct {
	// Glossary protects terms and answers whole ones; nil is no glossary at all.
	Glossary Glossary
	Memory   Answerer
	Cache    Answerer
	// Keeper holds what a source answered for ordinary text; nil keeps nothing.
	Keeper   Keeper
	Sources  Sources
	Settings func(ctx context.Context) settings.Values
	// Version reads the version when there is no Glossary; nil is version 0.
	Version func(ctx context.Context) (int, error)
	Logger  *slog.Logger
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
	// TermsNotPlaced is each published term a source lost, flagged for the reader: not put back,
	// as nothing says where it went (R-GLO-03).
	TermsNotPlaced []string
}

// Answer is every passage, in order.
type Answer struct {
	Passages []Passage
	// Sensitivity is what the text was treated as: what was stated, or personal where
	// ordinary text held an identity card number (R-MSK-04).
	Sensitivity string
	Mismatch    bool
	// From, To and GlossaryVersion are what the request was answered under (R-TRN-06).
	From, To        string
	GlossaryVersion int
}

// piece is one piece on its way through.
type piece struct {
	passage int
	before  string      // what lies between the last piece and this one, as it was
	masked  mask.Masked // with its values and any glossary terms stood in for
	info    PieceInfo
	text    string   // the finished piece, values restored
	lost    []string // published terms a source lost
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

// admit refuses what cannot be taken at all, reading nothing and sending nothing.
func (s *Service) admit(ctx context.Context, q Request) (settings.Values, *reasons.Failure) {
	v := s.Settings(ctx)
	if f := check(q, v); f != nil {
		return v, f
	}
	// There are no providers on provincial machines yet, so nothing can be done with
	// confidential text, whatever it holds (R-MSK-01). It goes nowhere, not even the
	// glossary's look-up.
	if q.Sensitivity == sources.Confidential {
		return v, reasons.New(reasons.NoProvincial)
	}
	return v, nil
}

// requestTime is the time one request may take (R-ERR-03).
func requestTime(ctx context.Context, v settings.Values) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, time.Duration(v.Of(settings.RequestLimitSeconds))*time.Second)
}

// Translate answers a request, whole or not at all. The failure is the one way out with
// no answer, and holds no text.
func (s *Service) Translate(ctx context.Context, q Request) (Answer, *reasons.Failure) {
	v, f := s.admit(ctx, q)
	if f != nil {
		return Answer{}, f
	}
	ctx, cancel := requestTime(ctx, v)
	defer cancel()
	w, f := s.prepare(ctx, q)
	if f != nil {
		return Answer{}, f
	}
	return w.Finish(ctx)
}

// Readiness says whether any allowed source could be called now, and if none could, the earliest
// one is expected. A background run asks, so it is refused at once where a direct request would
// fail at once (R-TRN-10, R-NFR-09).
type Readiness interface {
	Ready(ctx context.Context, from, to, sensitivity string) (time.Time, bool)
}

// Start checks a background run as a direct request would be checked, and answers what the glossary,
// memory and cache know, inside the time of one request (R-TRN-10). What it returns holds the run's
// text and masks in memory, and nowhere else (R-TRN-13). Finish does the rest, with a limit of the
// caller's.
func (s *Service) Start(ctx context.Context, q Request) (*Work, *reasons.Failure) {
	v, f := s.admit(ctx, q)
	if f != nil {
		return nil, f
	}
	ctx, cancel := requestTime(ctx, v)
	defer cancel()
	w, f := s.prepare(ctx, q)
	if f != nil {
		return nil, f
	}
	if ctx.Err() != nil {
		return nil, reasons.New(reasons.TimedOut)
	}
	if ready, ok := s.Sources.(Readiness); ok && len(w.need) > 0 {
		if at, yes := ready.Ready(ctx, w.q.From, w.q.To, w.q.Sensitivity); !yes {
			return nil, reasons.New(reasons.NoProvider).Until(at).At(w.need[0].passage)
		}
	}
	return w, nil
}

// Work is a request checked and split, with the pieces nothing knew still to go to a source.
type Work struct {
	s        *Service
	q        Request
	all      []*piece
	need     []*piece
	tails    []string
	version  int
	mismatch bool
	done     atomic.Int64
}

// Pieces is how many pieces are done and how many there are, safe to read while Finish runs. It
// says nothing of what they hold.
func (w *Work) Pieces() (done, total int) { return int(w.done.Load()), len(w.all) }

// prepare masks and splits the passages and answers what it can without a source.
func (s *Service) prepare(ctx context.Context, q Request) (*Work, *reasons.Failure) {
	version := 0
	var glossary *terms.Set
	var err error
	if s.Glossary != nil {
		version, glossary, err = s.Glossary.Terms(ctx, q.From, q.To)
	} else if s.Version != nil {
		version, err = s.Version(ctx)
	}
	if err != nil {
		s.log().Error("the glossary was not read", "request_ref", q.Ref)
		return nil, reasons.New(reasons.NoProvider)
	}

	// Each passage is masked before it is split, so a number written with spaces can
	// never be broken up and sent in parts. The stand-ins and values stay here.
	limit := s.Sources.MaxText()
	var all []*piece
	if q.Tally != nil {
		defer func() { q.Tally.add(all) }()
	}
	tails := make([]string, len(q.Passages))
	mismatch := false
	for i, text := range q.Passages {
		m := mask.Mask(text)
		// Ordinary text holding an identity card number is treated as personal, the
		// whole request with it: another passage may well name the same person.
		if q.Sensitivity == sources.Ordinary && m.HasCard() {
			mismatch = true
		}
		// Terms are stood in for before the split too, so a term with a full stop in it stays one.
		m = protect(m, glossary)
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
	if q.Tally != nil {
		q.Tally.Sensitivity, q.Tally.Mismatch = q.Sensitivity, mismatch
	}
	allowed, read := s.Sources.Allowed(ctx, q.From, q.To, q.Sensitivity)
	var need []*piece
	for _, p := range all {
		if f := s.known(ctx, q, p, allowed); f != nil {
			return nil, f
		}
		if p.info.Origin == "" {
			need = append(need, p)
		}
	}
	// A direction closed for every allowed source needs a person (R-GAT-03). A list
	// never read says nothing of the gate: that is no provider, below.
	if len(need) > 0 && read && len(allowed) == 0 {
		f := reasons.New(reasons.HumanRequired).At(need[0].passage)
		f.Terms = published(all)
		return nil, f
	}
	w := &Work{s: s, q: q, all: all, need: need, tails: tails, version: version, mismatch: mismatch}
	w.done.Store(int64(len(all) - len(need)))
	return w, nil
}

// Finish sends the pieces nothing knew to a source, in order, and joins the answer. The first
// failure stops the rest. ctx holds the limit: a request's or a run's.
func (w *Work) Finish(ctx context.Context) (Answer, *reasons.Failure) {
	q := w.q
	if q.Tally != nil {
		defer func() { q.Tally.add(w.all) }()
	}
	// One piece twice in a request goes to a source once: what comes back is masked, so
	// each restores its own values. It lasts only as long as this request.
	done := map[string]*sourceAnswer{}
	for _, p := range w.need {
		if f := w.s.fromSource(ctx, q, p, w.version, done); f != nil {
			return Answer{}, f.At(p.passage)
		}
		w.done.Add(1)
	}
	a := join(q, w.all, w.tails)
	a.Sensitivity, a.Mismatch, a.From, a.To, a.GlossaryVersion = q.Sensitivity, w.mismatch, q.From, q.To, w.version
	return a, nil
}

// protect stands in for each glossary term in a masked text.
func protect(m mask.Masked, glossary *terms.Set) mask.Masked {
	found := glossary.Find(m.Text)
	if len(found) == 0 {
		return m
	}
	list := make([]mask.Protection, len(found))
	for i, f := range found {
		list[i] = mask.Protection{Start: f.Start, End: f.End, Answer: f.Term.Target}
	}
	return m.Protect(list)
}

// published is each term found in the request, once, as it stands in the language asked for, to help
// the translator (R-GAT-03).
func published(all []*piece) []string {
	var out []string
	for _, p := range all {
		for _, it := range p.masked.Terms() {
			if !slices.Contains(out, it.Answer) {
				out = append(out, it.Answer)
			}
		}
	}
	return out
}

// wholeTerm says the piece holds glossary terms and no other words: it needs nobody else.
func wholeTerm(m mask.Masked) bool {
	return len(m.Terms()) > 0 && !strings.ContainsFunc(mask.WithoutStandIns(m.Text), unicode.IsLetter)
}

// known answers from the glossary, the memory and the cache, in that order (R-TRN-04). The memory
// holds what people approved, terms as they wrote them, so it is asked with terms as written. The
// cache holds pieces as they went to a source, so it is asked with terms stood in for.
func (s *Service) known(ctx context.Context, q Request, p *piece, allowed []bumblebee.Source) *reasons.Failure {
	if wholeTerm(p.masked) {
		text, _, _, err := restore(p.masked, p.masked.Text)
		if err != nil {
			return reasons.New(reasons.MaskingLost).At(p.passage)
		}
		p.text, p.info = text, PieceInfo{Origin: OriginGlossary}
		return nil
	}
	plain := p.masked.Unprotected()
	for _, a := range []struct {
		name string
		it   Answerer
		text string
	}{{OriginMemory, s.Memory, plain.Text}, {OriginCache, s.Cache, p.masked.Text}} {
		// Personal text is never cached (R-TRN-08), and not even looked for there: its
		// hash stays out of the database too.
		if a.it == nil || (a.name == OriginCache && q.Sensitivity != sources.Ordinary) {
			continue
		}
		l := Lookup{From: q.From, To: q.To, Sensitivity: q.Sensitivity, Text: a.text, Normal: Normalise(a.text), Plain: p.masked.Plain(), Allowed: allowed}
		hit, ok, err := a.it.Answer(ctx, l)
		if err != nil {
			s.log().Error("a piece could not be looked up", "request_ref", q.Ref, "from", a.name)
			continue
		}
		if !ok {
			continue
		}
		var text string
		var lost []string
		if a.name == OriginMemory {
			text, err = plain.Restore(hit.Text)
		} else {
			text, lost, _, err = restore(p.masked, hit.Text)
		}
		if err != nil {
			return reasons.New(reasons.MaskingLost).At(p.passage)
		}
		p.text, p.lost, p.info = text, lost, PieceInfo{Origin: hit.Origin, Source: hit.Source, Version: hit.Version}
		return nil
	}
	return nil
}

// A source may change a stand-in's brackets or spacing while keeping what is in it. Those it did not
// lose: they are put back (R-GLO-03).
var changedBrackets = regexp.MustCompile(`[⟦\[(〚【「{<]\s*[Gg]\s*(\d+)\s*[⟧\])〛】」}>]`)

// restore makes a finished piece of an answer: each term as published, then each value as it was,
// which is strict (R-MSK-03). A term the source lost is no failure but flagged for the reader, while
// a stand-in nobody can account for fails like a masked value. exact says every term came back whole.
func restore(m mask.Masked, answer string) (string, []string, bool, error) {
	var lost []string
	exact := true
	bare := m.Unprotected()
	for _, it := range m.Terms() {
		if strings.Contains(answer, it.Token) {
			continue
		}
		exact = false
		number := strings.TrimSuffix(strings.TrimPrefix(it.Token, "⟦G"), "⟧")
		found := false
		answer = changedBrackets.ReplaceAllStringFunc(answer, func(s string) string {
			if changedBrackets.FindStringSubmatch(s)[1] == number {
				found = true
				return it.Token
			}
			return s
		})
		if !found {
			lost = append(lost, it.Answer)
		}
	}
	for _, it := range m.Terms() {
		answer = strings.ReplaceAll(answer, it.Token, it.Answer)
	}
	text, err := bare.Restore(answer)
	return text, lost, exact, err
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
func (s *Service) fromSource(ctx context.Context, q Request, p *piece, version int, done map[string]*sourceAnswer) *reasons.Failure {
	got, seen := done[p.masked.Text]
	if !seen {
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
		text, lost, exact, err := restore(p.masked, got.result.Text)
		if err != nil {
			return s.lost(q)
		}
		p.text, p.lost = text, lost
		p.info = PieceInfo{Origin: OriginSource, Source: got.result.Source, Version: got.result.Version, Fallback: got.result.Fallback}
		if !seen && exact {
			s.keep(ctx, q, p, got.result.Text, got.result, version)
		}
		return nil
	}
	var b, kept strings.Builder
	last := 0
	sameSource := true
	for i, sp := range got.spans {
		text, lost, exact, err := restore(p.masked.Slice(sp.Start, sp.End), got.parts[i].Text)
		if err != nil {
			return s.lost(q)
		}
		p.lost = append(p.lost, lost...)
		sameSource = sameSource && exact
		between := p.masked.Text[last:sp.Start]
		b.WriteString(between)
		b.WriteString(text)
		kept.WriteString(between)
		kept.WriteString(renumbered(p.masked, sp, got.parts[i].Text))
		last = sp.End
		r := got.parts[i]
		sameSource = sameSource && r.Source == got.parts[0].Source && r.Version == got.parts[0].Version
		p.info = PieceInfo{Origin: OriginSource, Source: r.Source, Version: r.Version, Fallback: p.info.Fallback || r.Fallback}
	}
	b.WriteString(p.masked.Text[last:])
	kept.WriteString(p.masked.Text[last:])
	p.text = b.String()
	// Kept by source: a piece two sources made between them is not one source's.
	if !seen && sameSource {
		s.keep(ctx, q, p, kept.String(), got.parts[0], version)
	}
	return nil
}

// renumbered gives a part's answer the stand-in numbers of the whole piece, so it can be
// kept and restored with the piece. A part numbers its own from one; the piece's differ.
func renumbered(whole mask.Masked, sp Span, answer string) string {
	part := whole.Slice(sp.Start, sp.End)
	var pairs []string
	i := 0
	for _, it := range whole.Items {
		if it.Start >= sp.Start && it.End <= sp.End {
			pairs = append(pairs, part.Items[i].Token, it.Token)
			i++
		}
	}
	return strings.NewReplacer(pairs...).Replace(answer)
}

// keep holds a piece of ordinary text for the next time (R-TRN-08). Personal text never
// comes here; that includes ordinary text found to hold a card (R-MSK-04), as q's
// sensitivity has changed by now. A value is never in what is kept: only the stand-in.
func (s *Service) keep(ctx context.Context, q Request, p *piece, translation string, r sources.Result, version int) {
	if s.Keeper == nil || q.Sensitivity != sources.Ordinary {
		return
	}
	k := Keep{From: q.From, To: q.To, Normal: Normalise(p.masked.Text), Translation: translation, Source: r.Source, Version: r.Version, GlossaryVersion: version}
	if err := s.Keeper.Keep(ctx, k); err != nil {
		s.log().Error("a piece was not kept in the cache", "request_ref", q.Ref)
	}
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
		for _, l := range p.lost {
			if !slices.Contains(out.Passages[p.passage].TermsNotPlaced, l) {
				out.Passages[p.passage].TermsNotPlaced = append(out.Passages[p.passage].TermsNotPlaced, l)
			}
		}
	}
	for i := range q.Passages {
		builders[i].WriteString(tails[i])
		out.Passages[i].Text = builders[i].String()
	}
	return out
}
